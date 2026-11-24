# Running a CVN pool

This guide is for someone who wants to run a mining pool for others. To mine at a pool, read section 5 of [MINING_GUIDE.md](MINING_GUIDE.md).

A pool is the ordinary CVN program started with one more option. There is no separate pool software, no database server and no website to build. Anyone may run one.

Read all of this guide before you start. A pool's blocks pay the miners themselves, so most of what your miners earn never passes through your hands. The small parts that do are other people's coins, and section 7 says plainly what that asks of you.

## 1. What a pool does

```
./cvn_node --network cvn_testnet --pool CVN_POOL_ADDRESS
```

Started like this, the node:

- **Takes miners in from anywhere,** on the network's work port (18083 on the CVN Testnet, 28083 on `devnet`). Each miner logs in with its own CVN address and is asked for work its machine can do about once every 30 seconds. One such piece of work is a share.
- **Builds every block to pay the miners themselves.** A block's reward is divided among the addresses that did the recent work, and each part of 0.01 CVN or more is paid in the block, straight to its address, for up to 50 addresses. The pool never holds those coins. Smaller parts, and the parts of miners beyond the fifty, go to the pool's address in the same block and are owed.
- **Keeps a record** of every share, every block, what each address was paid by the blocks and what it is owed, in a file of its own beside the chain: `pool_cvn_testnet.db`.
- **Does not mine itself,** so the server's processor is free for checking shares.
- **Shows a pool page** at `/pool` (and the same figures for programs at `/pool.json`) on the port its explorer is on.
- **Holds no key.** It cannot spend the pool's coins. Paying is done by a separate program on a timer (section 4).

The rules are part of the program and the same for every pool run with it:

| | |
|---|---|
| How a block is divided | By the most recent shares, counted back until they add up to twice the work of one block. The division is fixed when a miner's work is made, and miners are sent fresh work at least every 15 seconds |
| What the block pays itself | Every part of 0.01 CVN or more, to up to 50 addresses (49 and the pool's own output, if there are more) |
| What the pool holds and owes | The parts under 0.01 CVN, and the parts of miners beyond those the block has room for |
| The pool's fee | None. There is no setting for one |
| When a block counts | When 100 more blocks stand on it, about 75 minutes. A block replaced before then counts for nothing, and its payments go with it |
| Smallest payment of what is owed | 1 CVN. Less stays owed until it grows |
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
- installs the timer that pays the miners every five hours. It does nothing until you have given it the pool's key, which you do once (section 4).

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

**Most of the paying is done by the blocks.** Every part of a reward of 0.01 CVN or more is paid in the block that earned it, and needs nothing from you. This section is about the rest: the small parts, and the parts of miners a block had no room for. They go to the pool's address and are owed. A pool with few miners may never owe anything.

**The pool pays what it owes by itself, every five hours.** Nobody runs a command to pay. Five hours after one payment run the next one starts, and so on for as long as the server is up; after a restart the first run comes fifteen minutes after the server starts. Each run pays every address owed 1 CVN or more.

For that the pool needs its key, and giving it the key is the one thing you do by hand, once.

### Setting it up, once

1. **Save the key on the server, locked with a password.** As root:

   ```bash
   cd /root/cvn_pool_keys/cvn_testnet && /opt/cvn_node/cvn_node --network cvn_testnet --wallet
   ```

   Choose 6, "Recovery words and saved key", then c, "Save a key on this computer". Type the pool's 24 words, then a password of your choosing. Use a long one that you use nowhere else. The key is written to `wallet_key.json` in that folder, locked.

2. **Put the password in a file only root can read:**

   ```bash
   ( umask 077; read -rsp 'Password: ' p; echo; printf '%s\n' "$p" > /root/cvn_pool_keys/cvn_testnet/password )
   ```

   The pool refuses a password file that other users of the machine can read.

3. **Try it once and read what it says:**

   ```bash
   systemctl start cvn-pool-pay-cvn_testnet; journalctl -u cvn-pool-pay-cvn_testnet -n 30 --no-pager
   ```

Until these three steps are done the pool pays nothing of what it owes: those amounts are recorded, and wait. What the blocks pay the miners themselves does not depend on this. The installer says so at the end of its output for as long as it is true.

| To do this | Type |
|---|---|
| See when the next payment run is | `systemctl list-timers cvn-pool-pay-cvn_testnet.timer` |
| See what the last run did | `journalctl -u cvn-pool-pay-cvn_testnet -n 30 --no-pager` |
| Pay now, without waiting | `systemctl start cvn-pool-pay-cvn_testnet` (the five hours then count from this run) |
| Pay at another interval | run the installer again with the hours named: `CVN_POOL_PAY_HOURS=2 bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS` |

The pool page lists every payment.

**Why five hours.** A shorter interval pays miners sooner and leaves less at the pool's address at any moment. A longer one makes fewer transfers, so less is taken from miners in fees and the chain carries less. Five hours is the middle chosen for the project's own pool; one hour to a day are all reasonable.

### What happens in a payment run

1. The paying command asks the pool's node, on the same machine, to prepare the payments: who is paid what, and from which of the pool's coins.
2. It checks that each payment hangs together, signs it with the pool's key, and hands it back.
3. The node checks that what came back is exactly what it prepared, writes it into the record as sent, and hands it to the network.
4. When the transfer is in a block with 100 more on it, the payment is settled.

The key is opened only for the seconds a run takes, by a program that listens to nobody. The node that listens to miners never has it. A payment that has been sent is never made a second time: if its transfer is slow to be mined, the node hands the same transfer to the network again every five minutes until it is. Only coins from blocks that count, and change from payments that have settled, are spent, so no payment can be undone by the chain changing.

### What this costs in safety, and paying by hand instead

With the key and its password both on the server, somebody who becomes root there can take every coin the pool's address holds. The node that listens to miners runs as another user and cannot read either file, so a fault in the node alone does not give the key away; a fault that gives an attacker root does. What stands to be lost is what the address holds at that moment: the small parts of the last five hours or so, and amounts under 1 CVN. What the blocks paid the miners themselves was never there to lose. Use the pool's wallet for the pool only, so that nothing else is ever at that address.

An operator who will not keep the key on a server can leave the three steps undone and pay by hand:

```bash
cd /root/cvn_pool_keys/cvn_testnet && /opt/cvn_node/cvn_node --network cvn_testnet --pool-pay
```

It asks for the pool's 24 words (they are not shown as you type), says what it is about to pay, and asks you to type YES. Nothing is kept on the server afterwards. Miners are then paid when you remember to do it, which is why it is not the usual way.

To stop automatic payments later: `systemctl disable --now cvn-pool-pay-cvn_testnet.timer`, then in the wallet choose 6 and d to remove the saved key, and delete the password file.

## 5. Looking after it

- **Watch the pool page.** "Paid to miners by the blocks themselves" should grow with every block that counts. "Owed" should fall after each payment run. "Held at the pool's address" should be close to what is owed plus what waiting blocks paid the pool.
- **Lost blocks are normal** in small numbers: two miners sometimes find a block at the same moment, and one of them loses. Many lost blocks mean the server is badly connected to the rest of the network.
- **The pool's record is one file,** `/var/lib/cvn_pool/NETWORK/pool_NETWORK.db`. It is the only place that says what each address is owed for work already done; the chain cannot tell you that. The pool makes copies of it by itself, and the next section is about them.
- **Keep the server updated:** `apt-get update && apt-get upgrade` for the system, and the installer again for the program when a release says so. Allow remote login with a key only, not a password.
- **If the pool stops,** miners' machines keep trying to connect and earn nothing meanwhile. What they were owed is still owed. Start it again as soon as you can and say what happened.
- **Closing a pool.** Tell your miners in advance, and name the day. On that day close the work port (`ufw delete allow 18083/tcp`) and restart the pool, so that no miner is connected. Leave it running about 75 minutes more, for the last blocks to count, then start a payment run (`systemctl start cvn-pool-pay-NETWORK`). Amounts under 1 CVN are not paid by the command; decide in advance what you will do about them and say so.

## 6. Copies of the pool's record, and losing it

**The pool copies its record by itself:** every hour, and straight after payments have been sent, into the folder `backups` beside the record (`/var/lib/cvn_pool/NETWORK/backups`). Each copy is taken through the database, so it is the record exactly as it stood at one moment, and it is checked before it is kept: all of it can be read, and it adds up. The newest 48 are kept. A copy is named after the moment it was made, in universal time: `pool_NETWORK_20261124-101500-00.db`.

If the pool's log ever says that a copy "did not pass its check", stop paying and find out why before you go on. The record itself does not add up.

**Copies on the server do not outlive the server.** Fetch them to another machine regularly; they are whole files and safe to copy while the pool runs. A copy holds no key. It holds what the pool page shows.

| To do this | Type |
|---|---|
| Check any copy | `/opt/cvn_node/cvn_node --network NETWORK --check-pool-backup FILE` |
| Put a copy in place of the record (pool stopped) | see below |
| Make no copies (not advised) | add `--no-backups` to the pool's command; it switches off the chain's backups too |

Each copy is the size of the record. That is a few megabytes for most pools, and can pass a hundred for a pool with very many miners and a small part of the network's speed, because such a pool keeps many hours of shares.

### If the record is lost or damaged

1. Stop the pool: `systemctl stop cvn-pool-NETWORK`
2. Put the newest good copy in place, as the pool's own user so that the pool can write to it:

   ```bash
   cd /var/lib/cvn_pool/NETWORK && sudo -u cvnpool /opt/cvn_node/cvn_node --network NETWORK --restore-pool-backup backups/pool_NETWORK_THE-NEWEST.db
   ```

   The copy is checked first. The file it replaces, if there is one, is kept beside it under another name.
3. Start the pool: `systemctl start cvn-pool-NETWORK`

**What the pool does then.** A copy knows nothing of what happened after it was made. Payments made since are on the chain, and the pool reads the chain for every transfer its address has sent. Before it pays anybody, it takes each one it did not know of into its record: what each address received comes off what the record says it is owed. Nobody is paid twice. Until every such transfer is 100 blocks deep, the pool prepares no payment, and the pool page says "Payments are waiting".

**What cannot be got back.** What the blocks paid the miners themselves is on the chain and is not touched by any of this. What can be lost is smaller: for blocks found after the copy was made, the record put back does not know whose the small parts are that went to the pool's address. Those coins stay there, outside what the record counts as owed. The same goes for shares handed in after the copy. Because a copy is made every hour and after every payment, this is the small parts of an hour's blocks at most, if you put back the newest copy.

Those coins are yours to settle by hand, and you should say publicly how you did it. The fairest simple way is to divide them among the same addresses, in the same proportions, as the last blocks the record does know. Send them from the pool's wallet in the ordinary way. The pool notices a transfer sent by hand from its address, waits until it is 100 blocks deep, and writes it into the record as a payment to whoever received it. If an address is paid more than the record says it was owed, the record owes it nothing, never less than nothing, and shows the difference with the payment.

**A transfer sent by hand at any other time** is treated the same way: as a payment to whoever received it. This is why the pool's wallet should be used for the pool and nothing else.

## 7. What you owe your miners

The program pays most of what your miners earn without you, and makes the rest of your pool's arithmetic public. It cannot make its operator honest. Two things remain in your hands: the small parts your pool holds until it pays them, and the work your pool hands out, which decides who each block pays. People who mine at your pool deserve to know these things before they start:

- **Who you are,** or at least how to reach you.
- **The pool's address,** so that anyone can see on the chain what it found and what it paid.
- **How often your pool pays.** Every five hours unless you have changed it.
- **Any change you have made to the program.** A pool run with the published program takes no fee and divides blocks as section 1 says. If yours is built from changed source, say what you changed.

And these you should hold yourself to:

- Use the pool's wallet for the pool and nothing else, so that its address tells the whole story.
- Never ask a miner for a private key or recovery words. A pool needs a miner's address only.
- If something goes wrong (a break-in, a lost record, a payment you cannot make), say so at once and in full.

One more thing, for the network's sake. A pool that grows to hold a large part of a network's mining speed is a danger to it, however honest its operator: whoever controls that pool decides which transfers a large share of the blocks carry, and at more than half could rewrite recent history. If your pool grows that large, ask miners to go elsewhere.

## 8. What the program does not do yet

- The connection between a miner and the pool is not encrypted.
- One pool is one server. Several servers cannot share one record.
- After an older copy of the record has been put back, the program cannot work out who earned the blocks found after that copy was made (section 6). That is left to the operator.
- Only the fast miner (the CVN edition of XMRig) can mine at a pool. The miner built into the CVN program cannot.
