#!/bin/sh
# Throwaway: measure how long Chrome takes to print "DevTools listening on"
# with chromedp's default flags, cold, warm and under the CI test load.
set -eu
cd -- "$(dirname "$0")/.."

chrome=
for c in headless_shell headless-shell chromium chromium-browser google-chrome google-chrome-stable; do
	if p=$(command -v "$c"); then
		chrome=$p
		break
	fi
done
echo "chromedp would use: $chrome"
"$chrome" --version

measure() {
	label=$1
	d=$(mktemp -d)
	out=$d.out
	s=$(date +%s.%N)
	"$chrome" --no-first-run --no-default-browser-check --headless \
		--disable-background-networking \
		--enable-features=NetworkService,NetworkServiceInProcess \
		--disable-background-timer-throttling --disable-backgrounding-occluded-windows \
		--disable-breakpad --disable-client-side-phishing-detection --disable-default-apps \
		--disable-dev-shm-usage --disable-extensions \
		--disable-features=site-per-process,Translate,BlinkGenPropertyTrees \
		--disable-hang-monitor --disable-ipc-flooding-protection --disable-popup-blocking \
		--disable-prompt-on-repost --disable-renderer-backgrounding --disable-sync \
		--force-color-profile=srgb --metrics-recording-only --safebrowsing-disable-auto-update \
		--enable-automation --password-store=basic --use-mock-keychain \
		--remote-debugging-port=0 --user-data-dir="$d" about:blank >"$out" 2>&1 &
	pid=$!
	t=timeout
	i=0
	while [ $i -lt 1200 ]; do
		if grep -q '^DevTools listening on' "$out"; then
			t=$(awk -v s="$s" -v e="$(date +%s.%N)" 'BEGIN{printf "%.2f", e - s}')
			break
		fi
		sleep 0.1
		i=$((i + 1))
	done
	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
	echo "MEASURE $label $t"
	case $t in
	timeout) cat "$out" ;;
	*) if awk -v t="$t" 'BEGIN{exit !(t > 5)}'; then cat "$out"; fi ;;
	esac
	rm -rf "$d" "$out"
}

measure cold
for i in 1 2 3; do measure "idle$i"; done

# Same load as ci/test.sh. CI is unset so TestWasm skips and does not start
# its own Chrome.
go install github.com/agnivade/wasmbrowsertest@8be019f6c6dceae821467b4c589eb195c2b761ce
env -u CI go test --race --bench=. ./... >/tmp/load.log 2>&1 &
load=$!
n=0
while kill -0 "$load" 2>/dev/null; do
	n=$((n + 1))
	measure "load$n"
	sleep 2
done
wait "$load" || echo "load run failed"
tail -n 20 /tmp/load.log
