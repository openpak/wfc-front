# Deploy: wfc-front

## Install (nothing changes for players yet)

1. Services on wfc-common v0.9.0 or later (wfc-gpcm v0.5.0, wfc-serverbrowser v0.3.0, wfc-qr2
   v0.5.0, wfc-natneg v0.2.0), each with its drop-in from `deploy/dropins/` (publishes the proxy
   port on `127.0.0.1` only and sets `WFC_PROXY_LISTEN`). Their direct ports keep working.
2. `deploy/openpak-wfc-front.container` in `~/.config/containers/systemd/`, image tag pinned,
   `systemctl --user daemon-reload && systemctl --user start openpak-wfc-front`.
3. Test the relay on its own ports before switching (from outside, or from the box itself).

The proxy ports are bound to the host's loopback, so only wfc-front (through pasta's `-T`/`-U`
forwards) can send PROXY-framed traffic; nobody on the internet can claim an address.

## Switch and rollback

```sh
deploy/switch.sh on       # 29900, 28910 (tcp), 27900, 27901 (udp) -> wfc-front
deploy/switch.sh off      # rollback: the redirects go, the services' own ports answer again
deploy/switch.sh status
```

`on` takes `~/openpak-deploy.lock`, first adds INPUT ACCEPT rules for the four relay ports (the
INPUT chain ends in a REJECT), then adds all four `nat PREROUTING` redirects in one
`iptables-restore` commit and the same lines to `/etc/iptables/rules.v4` (backup first).
`off` removes the redirects the same way. Port 80 and its rules are not touched.

UDP flows already open keep their old path until their conntrack entry expires (a minute or
two); new flows take the new one.

## Before and after

Run the game-port check before and after a switch and compare: a GPCM login greeting, a server
browser list request, a QR2 availability reply and a NATNEG init reply on the public ports; and
a QR2 heartbeat that carries the client's real public address, which QR2 drops before the switch
("Public IP mismatch") and accepts after it.
