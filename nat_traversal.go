package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// AUTOMATIC PORT MAPPING (UPnP)
//
// On start the node asks the home router, over UPnP, to forward the gossip
// port (8080) and the block-sync port (8081) to this machine, and renews the
// request while it runs. That is the same thing a user does by hand in the
// router's port-forwarding page.
//
// Plenty of routers have UPnP switched off, and no router setting helps behind
// carrier-grade NAT. In both cases the node still receives blocks through the
// outbound relay in relay_uplink.go; it just cannot serve other nodes.
//
// Set CVN_DISABLE_UPNP=1 to skip this entirely.

// upnpMappedPorts are the ports a router is asked to forward to this node.
func upnpMappedPorts() []int { return []int{Params.GossipPort, Params.SyncPort} }

func natPortsText() string { return fmt.Sprintf("%d-%d", Params.GossipPort, Params.SyncPort) }

const (
	upnpLeaseSeconds  = 3600
	upnpRenewInterval = 20 * time.Minute
	upnpRetryInterval = 30 * time.Minute
)

// announceNetworkStatus prints a line and also places it in the dashboard's
// event list, where it survives the next screen redraw.
func announceNetworkStatus(line string) {
	log.Println(line)
	logMutex.Lock()
	liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line))
	logMutex.Unlock()
}

// SetupAutomatedPortMapping starts the background port-mapping routine.
func SetupAutomatedPortMapping() {
	go func() {
		// Let the startup banner print first.
		time.Sleep(500 * time.Millisecond)

		if os.Getenv("CVN_DISABLE_UPNP") != "" {
			announceNetworkStatus("📡 [NAT] Automatic port mapping disabled (CVN_DISABLE_UPNP). Blocks still arrive through the outbound relay.")
			return
		}
		localIP := detectLocalIPv4()
		if localIP == nil {
			return
		}
		if !isBehindNATAddress(localIP) {
			announceNetworkStatus(fmt.Sprintf("📡 [NAT] This machine has a public address (%s); no router mapping needed.", localIP))
			return
		}

		announcedSuccess, announcedFailure := false, false
		for {
			externalIP, err := mapPortsViaUPnP(localIP)
			if err == nil {
				if !announcedSuccess {
					announcedSuccess, announcedFailure = true, false
					if parsed := net.ParseIP(externalIP); parsed != nil && isBehindNATAddress(parsed) {
						announceNetworkStatus(fmt.Sprintf("⚠️ [NAT] Router mapped ports "+natPortsText()+", but its own address (%s) is private: your ISP uses carrier-grade NAT, so peers still cannot connect in. Running in outbound relay mode.", externalIP))
					} else {
						announceNetworkStatus(fmt.Sprintf("✅ [NAT] Router mapped ports "+natPortsText()+" to this machine automatically (public IP %s). No manual port forwarding needed.", externalIP))
					}
				}
				time.Sleep(upnpRenewInterval)
				continue
			}
			if !announcedFailure {
				announcedFailure, announcedSuccess = true, false
				announceNetworkStatus(fmt.Sprintf("📡 [NAT] Automatic port mapping unavailable (%v). Running in outbound relay mode: you still receive blocks and can mine. Forward ports "+natPortsText()+" by hand if you want other nodes to sync from you.", err))
			}
			time.Sleep(upnpRetryInterval)
		}
	}()
}

// detectLocalIPv4 returns the address this machine uses to reach the internet.
func detectLocalIPv4() net.IP {
	// Connecting a UDP socket sends nothing; it only asks the OS which local
	// address it would route from. 192.0.2.1 is a reserved documentation address.
	if probe, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		defer probe.Close()
		if local, ok := probe.LocalAddr().(*net.UDPAddr); ok && local.IP.To4() != nil && !local.IP.IsLoopback() {
			return local.IP.To4()
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.To4()
		}
	}
	return nil
}

var carrierGradeNATBlock = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isBehindNATAddress reports whether an address is one the internet cannot reach directly.
func isBehindNATAddress(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || carrierGradeNATBlock.Contains(ip)
}

// mapPortsViaUPnP finds the router and maps every port in upnpMappedPorts.
// It returns the router's external IP as the router reports it.
func mapPortsViaUPnP(localIP net.IP) (string, error) {
	controlURL, serviceType, err := discoverUPnPGateway(localIP)
	if err != nil {
		return "", err
	}
	for _, port := range upnpMappedPorts() {
		if err := upnpAddPortMapping(controlURL, serviceType, port, localIP.String()); err != nil {
			return "", fmt.Errorf("router refused port %d: %v", port, err)
		}
	}
	externalIP, _ := upnpGetExternalIP(controlURL, serviceType)
	return externalIP, nil
}

// discoverUPnPGateway broadcasts an SSDP search on the LAN and returns the
// control URL and service type of the first router that answers.
func discoverUPnPGateway(localIP net.IP) (string, string, error) {
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP})
	if err != nil {
		return "", "", errors.New("cannot open a LAN socket")
	}
	defer socket.Close()

	ssdpAddress := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	for _, target := range []string{
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANIPConnection:2",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
	} {
		search := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + target + "\r\n\r\n"
		_, _ = socket.WriteToUDP([]byte(search), ssdpAddress)
	}

	_ = socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	tried := make(map[string]bool)
	buffer := make([]byte, 4096)
	for {
		n, sender, err := socket.ReadFromUDP(buffer)
		if err != nil {
			break
		}
		location := ssdpHeader(string(buffer[:n]), "location")
		if location == "" || tried[location] {
			continue
		}
		tried[location] = true
		// Only trust a description served by the LAN device that answered.
		parsed, perr := url.Parse(location)
		if perr != nil || parsed.Hostname() != sender.IP.String() || !isBehindNATAddress(sender.IP) {
			continue
		}
		if controlURL, serviceType, derr := upnpFetchControlURL(location); derr == nil {
			return controlURL, serviceType, nil
		}
	}
	return "", "", errors.New("no UPnP router answered; UPnP is probably switched off on the router")
}

func ssdpHeader(response string, name string) string {
	for _, line := range strings.Split(response, "\r\n") {
		if colon := strings.Index(line, ":"); colon > 0 && strings.EqualFold(strings.TrimSpace(line[:colon]), name) {
			return strings.TrimSpace(line[colon+1:])
		}
	}
	return ""
}

var upnpHTTPClient = &http.Client{Timeout: 5 * time.Second}

// upnpFetchControlURL reads the router's device description and returns the
// control URL of its WAN connection service.
func upnpFetchControlURL(location string) (string, string, error) {
	resp, err := upnpHTTPClient.Get(location)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}

	decoder := xml.NewDecoder(bytes.NewReader(body))
	var urlBase, element, serviceType, controlPath string
	var foundType, foundPath string
	for {
		token, terr := decoder.Token()
		if terr != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			element = t.Name.Local
			if element == "service" {
				serviceType, controlPath = "", ""
			}
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}
			switch element {
			case "URLBase":
				urlBase = text
			case "serviceType":
				serviceType = text
			case "controlURL":
				controlPath = text
			}
		case xml.EndElement:
			if t.Name.Local == "service" && foundType == "" &&
				(strings.Contains(serviceType, ":WANIPConnection:") || strings.Contains(serviceType, ":WANPPPConnection:")) && controlPath != "" {
				foundType, foundPath = serviceType, controlPath
			}
			element = ""
		}
	}
	if foundType == "" {
		return "", "", errors.New("router description lists no WAN connection service")
	}
	base, err := url.Parse(location)
	if err != nil {
		return "", "", err
	}
	if urlBase != "" {
		if parsedBase, berr := url.Parse(urlBase); berr == nil && parsedBase.Hostname() == base.Hostname() {
			base = parsedBase
		}
	}
	control, err := base.Parse(foundPath)
	if err != nil {
		return "", "", err
	}
	return control.String(), foundType, nil
}

func upnpSOAPCall(controlURL, serviceType, action, arguments string) (int, string, error) {
	envelope := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + serviceType + `">` + arguments + `</u:` + action + `></s:Body></s:Envelope>`
	request, err := http.NewRequest("POST", controlURL, strings.NewReader(envelope))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	request.Header.Set("SOAPAction", `"`+serviceType+`#`+action+`"`)
	resp, err := upnpHTTPClient.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, string(body), nil
}

func upnpAddPortMapping(controlURL, serviceType string, port int, localIP string) error {
	attempt := func(leaseSeconds int) (int, string, error) {
		arguments := fmt.Sprintf("<NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>TCP</NewProtocol>"+
			"<NewInternalPort>%d</NewInternalPort><NewInternalClient>%s</NewInternalClient><NewEnabled>1</NewEnabled>"+
			"<NewPortMappingDescription>Covenant Standard node</NewPortMappingDescription><NewLeaseDuration>%d</NewLeaseDuration>",
			port, port, localIP, leaseSeconds)
		return upnpSOAPCall(controlURL, serviceType, "AddPortMapping", arguments)
	}
	status, body, err := attempt(upnpLeaseSeconds)
	if err != nil {
		return err
	}
	if status != http.StatusOK && strings.Contains(body, "<errorCode>725</errorCode>") {
		// This router only accepts mappings without an expiry.
		status, body, err = attempt(0)
		if err != nil {
			return err
		}
	}
	if status != http.StatusOK {
		if start := strings.Index(body, "<errorDescription>"); start >= 0 {
			if end := strings.Index(body[start:], "</errorDescription>"); end > 0 {
				return errors.New(body[start+len("<errorDescription>") : start+end])
			}
		}
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func upnpGetExternalIP(controlURL, serviceType string) (string, error) {
	status, body, err := upnpSOAPCall(controlURL, serviceType, "GetExternalIPAddress", "")
	if err != nil || status != http.StatusOK {
		return "", errors.New("router did not report its external address")
	}
	start := strings.Index(body, "<NewExternalIPAddress>")
	end := strings.Index(body, "</NewExternalIPAddress>")
	if start < 0 || end < start {
		return "", errors.New("router did not report its external address")
	}
	return strings.TrimSpace(body[start+len("<NewExternalIPAddress>") : end]), nil
}
