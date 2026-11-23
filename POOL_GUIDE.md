# Running a CVN pool

This guide is for someone who wants to run a mining pool for others. To mine at a pool, read section 5 of [MINING_GUIDE.md](MINING_GUIDE.md).

A pool is the ordinary CVN program started with one more option. There is no separate pool software, no database server and no website to build. Anyone may run one.

Read all of this guide before you start. Running a pool means holding other people's coins for an hour or two at a time, and section 6 says plainly what that asks of you.

## 1. What a pool does

```
./cvn_node --network cvn_testnet --pool CVN_POOL_ADDRESS
```

Started like this, the node:

- **Takes miners in from anywhere,** on the network's work port (18083 on the CVN Testnet, 28083 on `devnet`). Each miner logs in with its own CVN address and is asked for work its machine can do about once every 30 seconds. One such piece of work is a share.
- **Builds every block to pay the pool's address.** When a miner's share is also good enough for the network, it is a block.
- **Keeps a record** of every share, every block and what each address is owed, in a file of its own beside the chain: `pool_cvn_testnet.db`.
- **Does not mine itself,** so the server's processor is free for checking shares.
- **Shows a pool page** at `/pool` (and the same figures for programs at `/pool.json`) on the port its explorer is on.
- **Holds no key.** It cannot spend the pool's coins. Paying is a separate command (section 4).

The rules are part of the program and the same for every pool run with it:

| | |
|---|---|
| How a block is divided | By the most recent shares, counted back until they add up to twice the work of one block |
| The pool's fee | None. There is no setting for one |
| When a block counts | When 100 more blocks stand on it, about 75 minutes. A block replaced before then counts for nothing |
| Smallest payment | 1 CVN. Less stays owed until it grows |
| Fee on a payment | 0.001 CVN for each transfer, shared equally among the addresses it pays |
| Addresses in one transfer | Up to 49 |
| Connections from one internet address | 32 |
| Miners at one pool | 2,000 |

How these were chosen, and what was considered and set aside, is in [POOL_DESIGN.md](POOL_DESIGN.md).

## 2. What you need

- **A server that is always on,** with Ubuntu, a fixed internet address and its ports open to the world. A pool takes no more disk than any node. How many miners one server carries has not been measured yet: checking a share costs the server a few hundredths of a second of one processor core, and each miner sends about two a minute. Two cores and 2 GB of memory is a sensible start.
- **A name for it** (optional), such as `pool.example.org`, pointing at the server. Miners can use the server's number instead.
- **A wallet for the pool,** made on a computer of your own and not on the server. Use it for nothing else. Write its 24 recovery words on paper.
- **Time.** A pool that stops paying, or goes offline for a day, costs its miners money. Do not start one you will not look after.

**Make the pool's wallet first,** on your own computer:

```
cvn_miner.exe --network cvn_testnet --wallet          (Windows)
./cvn_node --network cvn_testnet --wallet             (Linux)
```

Choose 5, "Create a new wallet". Answer **n** when it asks whether to use the address for mining on that machine. Write the 24 words down. The address it shows, beginning `CVN_`, is the pool's address: it is public, and it is what you give the server.

## 3. Setting the server up

On a fresh Ubuntu server, as root:

```bash
curl -fsSLO https://raw.githubusercontent.com/kalashnikovski/cvn_node/main/deploy/install_pool.sh
```

```bash
bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS
```

On `devnet`, which has no seeds of its own, add the other machines' addresses: `bash install_pool.sh devnet CVN_POOL_ADDRESS HOST,HOST,...`

The script:

- builds the program in `/opt/cvn_node`;
- makes a user named `cvnpool` for the node to run as. That user can write in the pool's own folder, `/var/lib/cvn_pool/NETWORK`, and nowhere else, and cannot read root's files;
- opens the network's two ports and the work port in the firewall;
- installs the pool as a service, `cvn-pool-NETWORK`, that starts when the server boots and restarts if it stops;
- installs a timer for payments, which does nothing until you set payments up (section 4).

Running it again later with the same words updates the program and restarts the pool. The chain, the pool's record and the saved key are kept.

| To do this | Type |
|---|---|
| See what the pool is doing | `journalctl -u cvn-pool-cvn_testnet -f` |
| Stop it | `systemctl stop cvn-pool-cvn_testnet` |
| Start it | `systemctl start cvn-pool-cvn_testnet` |
| See the pool page | open `http://YOUR-SERVER:18081/pool` in a browser |

**A name and a padlock for the pool page** (optional): `bash /opt/cvn_node/deploy/install_https.sh pool.example.org cvn_testnet` puts the page at `https://pool.example.org/pool`. The name must already point at the server. This is for the page only; miners connect to the work port as before.

**Tell your miners** the server's name or number, and that they start with `--pool-mine` followed by it. The pool page says the same.

## 4. Paying the miners

Nothing is paid until you pay. What each address has earned is recorded and owed, and waits.

```
./cvn_node --network cvn_testnet --pool-pay
```

run on the pool's server, pays every address owed 1 CVN or more. It works like this:

1. The command asks the pool's node, on the same machine, to prepare the payments: who is paid what, and from which of the pool's coins.
2. It checks that each payment hangs together, signs it with the pool's key, and hands it back.
3. The node checks that what came back is exactly what it prepared, writes it into the record as sent, and hands it to the network.
4. When the transfer is in a block with 100 more on it, the payment is settled.

The key is in the computer's memory only for the seconds the command runs. A payment that has been sent is never made a second time: if its transfer is slow to be mined, the node hands the same transfer to the network again every five minutes until it is. Only coins from blocks that count, and change from payments that have settled, are spent, so no payment can be undone by the chain changing.

There are two ways to run it. Choose one.

### By hand

Log in to the server and run:

```bash
cd /root/cvn_pool_keys/cvn_testnet && /opt/cvn_node/cvn_node --network cvn_testnet --pool-pay
```

It asks for the pool's 24 words (they are not shown as you type), says what it is about to pay, and asks you to type YES. Nothing is kept on the server afterwards.

This is the safer way: somebody who breaks into the server finds no key there. It is also the way that depends on you. Miners are paid when you remember to do it.

### By a timer

The server pays by itself, once an hour. For that the pool's key has to be on the server, and so does the password that opens it.

1. **Save the key on the server, locked with a password.** As root:

   ```bash
   cd /root/cvn_pool_keys/cvn_testnet && /opt/cvn_node/cvn_node --network cvn_testnet --wallet
   ```

   Choose 6, "Recovery words and saved key", then c, "Save a key on this computer". Type the pool's 24 words, then a password of your choosing. Use a long one that you use nowhere else. The key is written to `wallet_key.json` in that folder, locked.

2. **Put the password in a file only root can read:**

   ```bash
   ( umask 077; read -rsp 'Password: ' p; echo; printf '%s\n' "$p" > /root/cvn_pool_keys/cvn_testnet/password )
   ```

   The paying command refuses a password file that other users of the machine can read.

3. **Try it once and read what it says:**

   ```bash
   systemctl start cvn-pool-pay-cvn_testnet; journalctl -u cvn-pool-pay-cvn_testnet -n 30 --no-pager
   ```

From then on the timer runs it every hour. `systemctl list-timers` shows when it will next run, and the pool page lists every payment.

To pay at another interval, run the installer again with `CVN_POOL_PAY_EVERY` set, for example `CVN_POOL_PAY_EVERY=daily bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS`. To stop automatic payments: `systemctl disable --now cvn-pool-pay-cvn_testnet.timer`, then in the wallet choose 6 and d to remove the saved key, and delete the password file.

**What the timer costs in safety.** With the key and its password both on the server, somebody who becomes root there can take every coin the pool's address holds. The node that listens to miners runs as another user and cannot read either file, so a fault in the node alone does not give the key away; a fault that gives an attacker root does. What stands to be lost is what the address holds at that moment: blocks found in the last 75 minutes, payments not yet made, and amounts under 1 CVN. Paying every hour keeps that small. It is never nothing.

## 5. Looking after it

- **Watch the pool page.** "Owed" should fall after each payment run. "Held at the pool's address" should be close to what is owed plus the blocks still waiting.
- **Lost blocks are normal** in small numbers: two miners sometimes find a block at the same moment, and one of them loses. Many lost blocks mean the server is badly connected to the rest of the network.
- **The pool's record is one file,** `/var/lib/cvn_pool/NETWORK/pool_NETWORK.db`. It is the only place that says what each address is owed for work already done; the chain cannot tell you that. The program does not yet make a copy of it. Make your own: stop the pool (`systemctl stop cvn-pool-NETWORK`), copy the file somewhere off the server, and start the pool again. A pool that loses this file still has its coins and no longer knows whose they are.
- **Keep the server updated:** `apt-get update && apt-get upgrade` for the system, and the installer again for the program when a release says so. Allow remote login with a key only, not a password.
- **If the pool stops,** miners' machines keep trying to connect and earn nothing meanwhile. What they were owed is still owed. Start it again as soon as you can and say what happened.
- **Closing a pool.** Tell your miners in advance, and name the day. On that day close the work port (`ufw delete allow 18083/tcp`) and restart the pool, so that no miner is connected. Leave it running about 75 minutes more, for the last blocks to count, then run the payments. Amounts under 1 CVN are not paid by the command; decide in advance what you will do about them and say so.

## 6. What you owe your miners

The program makes a pool's arithmetic public. It cannot make its operator honest. People who mine at your pool are trusting you with their earnings, and deserve to know these things before they start:

- **Who you are,** or at least how to reach you.
- **The pool's address,** so that anyone can see on the chain what it found and what it paid.
- **How often you pay,** and whether by hand or by a timer.
- **Any change you have made to the program.** A pool run with the published program takes no fee and divides blocks as section 1 says. If yours is built from changed source, say what you changed.

And these you should hold yourself to:

- Use the pool's wallet for the pool and nothing else, so that its address tells the whole story.
- Never ask a miner for a private key or recovery words. A pool needs a miner's address only.
- If something goes wrong (a break-in, a lost record, a payment you cannot make), say so at once and in full.

One more thing, for the network's sake. A pool that grows to hold a large part of a network's mining speed is a danger to it, however honest its operator: whoever controls that pool decides which transfers a large share of the blocks carry, and at more than half could rewrite recent history. If your pool grows that large, ask miners to go elsewhere.

## 7. What the program does not do yet

- The connection between a miner and the pool is not encrypted.
- One pool is one server. Several servers cannot share one record.
- The pool's record is not backed up by the program (section 5).
- Only the fast miner (the CVN edition of XMRig) can mine at a pool. The miner built into the CVN program cannot.
