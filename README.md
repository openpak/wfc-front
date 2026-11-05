# wfc-front

OpenPak's Wii/DS game-port front.

| | |
| --- | --- |
| Role | Gives the address-sensitive WFC services their players' real public addresses |
| Language | Go 1.26, no dependencies |
| Image | `ghcr.io/openpak/wfc-front` |
| Licence | AGPL-3.0-only |

Rootless Podman's port forwarder rewrites the source of every connection and datagram to a
container-network address. The WFC game services need the real one: QR2 drops a Wii's heartbeat
when the address the console reports differs from the one it sees ("Public IP mismatch"),
NATNEG tells two players each other's addresses, GPCM hands the address to QR2, and the server
browser answers "self" lookups by it (openpak/prds#149).

wfc-front runs on a pasta network (it sees players as they are, like openpak-edge) and relays
with PROXY protocol v2 to a second, loopback-only port of each service:

| Public port (players) | wfc-front | Service proxy port | Service |
| --- | --- | --- | --- |
| 29900/tcp | 39900 | 29950 | wfc-gpcm |
| 28910/tcp | 38910 | 28950 | wfc-serverbrowser |
| 27900/udp | 37900 | 27950 | wfc-qr2 |
| 27901/udp | 37901 | 27951 | wfc-natneg |

- TCP: a v2 header at the start of each connection, then bytes both ways (GPCM speaks first).
- UDP: each datagram from a player is framed with a v2 header naming the player; what the
  service sends is framed with the player as destination, and wfc-front sends it from the public
  port. That covers replies and unsolicited packets (QR2 to a host, NATNEG to both peers).

The services take the framed traffic with wfc-common v0.9.0 (`common.ProxyListener`,
`common.ListenGameUDP`, enabled by `WFC_PROXY_LISTEN`) and keep answering their direct ports, so
switching the public ports to wfc-front and back is safe at any time.

## Configuration

| Variable | Example |
| --- | --- |
| `FRONT_TCP` | `39900=127.0.0.1:29950,38910=127.0.0.1:28950` |
| `FRONT_UDP` | `37900=127.0.0.1:27950,37901=127.0.0.1:27951` |

## Logs

One line per TCP connection (duration and bytes) and, per port, one summary a minute (datagrams
in and out, connections, distinct players, dropped). No player addresses: the services log
those.

## Docs

- [Deploy](docs/deploy.md): install, the atomic switch and the one-command rollback
- [Changelog](CHANGELOG.md)
