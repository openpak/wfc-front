#!/bin/bash
# Send the Wii/DS game ports through wfc-front (real player addresses) or back to the services'
# own published ports, atomically, and persist it. Runs on the box under the deploy lock.
#
#   switch.sh on     GPCM 29900, server browser 28910 (tcp), QR2 27900, NATNEG 27901 (udp)
#                    -> wfc-front 39900, 38910, 37900, 37901
#   switch.sh off    rollback: remove those redirects (the services' ports answer directly again)
#   switch.sh status
set -euo pipefail
RULES=(
	"-p tcp -m tcp --dport 29900 -j REDIRECT --to-ports 39900"
	"-p tcp -m tcp --dport 28910 -j REDIRECT --to-ports 38910"
	"-p udp -m udp --dport 27900 -j REDIRECT --to-ports 37900"
	"-p udp -m udp --dport 27901 -j REDIRECT --to-ports 37901"
)
ACCEPTS=(
	"-p tcp -m tcp --dport 39900 -j ACCEPT"
	"-p tcp -m tcp --dport 38910 -j ACCEPT"
	"-p udp -m udp --dport 37900 -j ACCEPT"
	"-p udp -m udp --dport 37901 -j ACCEPT"
)
SAVED=/etc/iptables/rules.v4
show() {
	echo "live:"
	sudo -n iptables -t nat -S PREROUTING | grep -E -- "--dport (29900|28910|27900|27901) " || echo "  (no game-port redirects)"
	echo "saved:"
	sudo -n grep -E -- "--dport (29900|28910|27900|27901) -j REDIRECT" "$SAVED" || echo "  (no game-port redirects)"
}
case "${1:-}" in
status) show; exit 0 ;;
on | off) ;;
*) echo "usage: $0 on|off|status" >&2; exit 2 ;;
esac
exec 9>~/openpak-deploy.lock
flock 9
sudo -n cp -p "$SAVED" "$SAVED.before-wfc-front-$1-$(date +%Y%m%dT%H%M%S)"
if [ "$1" = on ]; then
	systemctl --user is-active --quiet openpak-wfc-front.service || { echo "wfc-front is not running" >&2; exit 1; }
	# 1. The relay ports must pass INPUT (it ends in a REJECT) before anything is sent to them.
	anchor=$(sudo -n iptables -L INPUT --line-numbers -n | awk '/dpt:21081/ && /ACCEPT/ {print $1; exit}')
	for a in "${ACCEPTS[@]}"; do
		if ! sudo -n iptables -C INPUT $a 2>/dev/null; then
			sudo -n iptables -I INPUT "${anchor:-1}" $a
		fi
		if ! sudo -n grep -q -- "^-A INPUT $a\$" "$SAVED"; then
			sudo -n sed -i "0,/^-A INPUT -p tcp -m tcp --dport 21081 -j ACCEPT\$/s//&\n-A INPUT $a/" "$SAVED"
		fi
	done
	# 2. All four redirects in one commit.
	{
		echo "*nat"
		for r in "${RULES[@]}"; do
			sudo -n iptables -t nat -C PREROUTING $r 2>/dev/null || echo "-A PREROUTING $r"
		done
		echo "COMMIT"
	} | sudo -n iptables-restore --noflush
	for r in "${RULES[@]}"; do
		if ! sudo -n grep -q -- "^-A PREROUTING $r\$" "$SAVED"; then
			sudo -n sed -i "0,/^-A PREROUTING -p tcp -m tcp --dport 80 -j REDIRECT --to-ports [0-9]*\$/s//&\n-A PREROUTING $r/" "$SAVED"
		fi
	done
else
	{
		echo "*nat"
		for r in "${RULES[@]}"; do
			while sudo -n iptables -t nat -C PREROUTING $r 2>/dev/null; do
				echo "-D PREROUTING $r"
				break
			done
		done
		echo "COMMIT"
	} | sudo -n iptables-restore --noflush
	for r in "${RULES[@]}"; do
		esc=$(printf '%s' "$r" | sed 's/[][\/.*^$]/\\&/g')
		sudo -n sed -i "/^-A PREROUTING $esc\$/d" "$SAVED"
	done
fi
show
