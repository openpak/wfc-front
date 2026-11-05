# Changelog

## v0.1.0 - 2026-11-05

- First release (openpak/prds#149): pasta-networked relay for the address-sensitive Wii/DS game ports (GPCM, server browser over TCP; QR2, NATNEG over UDP) that passes the player's real address with PROXY protocol v2 (a header per TCP connection, a frame per UDP datagram, both directions, so unsolicited packets work). Pinned quadlet, service drop-ins and `deploy/switch.sh on|off|status` (atomic, firewall first, one-command rollback).
