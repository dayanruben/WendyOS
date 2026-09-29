#!/usr/bin/env bash
# Probes how macOS 26 Spotlight + the FSKit FAT driver treat a re-flashed card,
# using a raw disk image in place of an SD card. Throwaway experiment for the
# SD reflash hang; runs on a disposable CI runner only (it toggles Spotlight).
#
# Usage: macos-fskit-experiment.sh <index|repro|fix-marker> <outdir>
#   index       does .metadata_never_index / `mdutil -i off` survive a re-attach?
#   repro       re-flash a mounted card while Spotlight indexes it: does
#               unmount or write hang?
#   fix-marker  repro on a card that already carries the marker (a card the
#               fixed CLI flashed), so Spotlight is left to honour it
#
# Bash 3.2 compatible (the macOS system bash).
# Logs are written as the runner user, and `set -- $mps` splits on purpose.
# shellcheck disable=SC2009,SC2024,SC2086
set -uo pipefail

MODE=${1:?mode}
OUT=${2:?outdir}
ITERATIONS=${ITERATIONS:-5}
mkdir -p "$OUT"
SUMMARY="$OUT/summary.md"
WORK=$(mktemp -d /tmp/fskit-exp.XXXXXX)

note() { echo "$*" | tee -a "$SUMMARY"; }

# run_bounded SECS LOG CMD... runs CMD in the background and gives up after
# SECS. A write blocked in the kernel cannot be killed, so it never waits on a
# hung process: it records diagnostics, fires a kill, and moves on. rc 124 = hung.
run_bounded() {
	local secs=$1 log=$2
	shift 2
	"$@" >"$log" 2>&1 &
	local pid=$! waited=0
	while kill -0 "$pid" 2>/dev/null; do
		if [ "$waited" -ge "$secs" ]; then
			diagnose "$log.hang"
			pkill -9 -P "$pid" 2>/dev/null || sudo -n pkill -9 -P "$pid" 2>/dev/null
			kill -9 "$pid" 2>/dev/null || sudo -n kill -9 "$pid" 2>/dev/null
			return 124
		fi
		sleep 1
		waited=$((waited + 1))
	done
	wait "$pid"
}

# outcome RC renders a run_bounded exit code for the results table.
outcome() {
	case $1 in
	0) echo ok ;;
	124) echo HUNG ;;
	*) echo "failed rc=$1" ;;
	esac
}

diagnose() {
	{
		echo "== processes"
		ps -axo pid,ppid,stat,wchan,etime,command | grep -Ei 'diskutil|[ /]dd |fskit|msdos|mds|hdiutil' | grep -v grep
		echo "== mounts"
		mount
		echo "== unified log (fskit/msdos/diskarbitration, last 3m)"
		log show --last 3m --style compact \
			--predicate 'process CONTAINS[c] "msdos" OR process CONTAINS[c] "fskit" OR subsystem CONTAINS[c] "fskit" OR process == "diskarbitrationd"' 2>&1 | tail -200
	} >"$1" 2>&1
	for p in $(pgrep -f 'diskutil|fskit|msdos' 2>/dev/null); do
		sudo sample "$p" 2 -file "$1.sample.$p" >/dev/null 2>&1
	done
}

# attach IMG [-nomount] prints the whole-disk node (/dev/diskN).
attach() {
	hdiutil attach -imagekey diskimage-class=CRawDiskImage "$@" | awk 'NR==1{print $1}'
}

DETACHES=0

# detach DISK; rc 124 = hung, anything else non-zero = refused (e.g. busy).
detach() {
	DETACHES=$((DETACHES + 1))
	local log="$OUT/detach-$DETACHES.log" rc
	run_bounded 30 "$log" hdiutil detach "$1"
	rc=$?
	[ $rc -eq 0 ] && return 0
	diagnose "$log.fail"
	run_bounded 30 "$log.force" hdiutil detach -force "$1"
}

# force_index MP... makes Spotlight index the volumes now: the runner does not
# index disk images on its own, and without indexing there is nothing to hang.
force_index() {
	sudo -n mdutil -i on "$@" >>"$OUT/force-index.log" 2>&1
	sudo -n mdutil -E "$@" >>"$OUT/force-index.log" 2>&1
}

# vol_dev DISK NAME prints the slice holding the volume named NAME.
vol_dev() {
	diskutil list "$1" | awk -v n="$2" '$0 ~ " "n" " {print "/dev/"$NF}'
}

mountpoint_of() {
	diskutil info -plist "$1" 2>/dev/null | plutil -extract MountPoint raw -o - - 2>/dev/null
}

# wait_mounted DISK prints BOOT and CONFIG mount points once both are up.
wait_mounted() {
	local i b c
	for i in $(seq 1 30); do
		b=$(mountpoint_of "$(vol_dev "$1" BOOT)")
		c=$(mountpoint_of "$(vol_dev "$1" CONFIG)")
		if [ -n "$b" ] && [ -n "$c" ]; then
			echo "$b $c"
			return 0
		fi
		sleep 1
	done
	return 1
}

index_state() {
	mdutil -s "$1" 2>&1 | sed -n 2p | tr -d '\t'
}

# make_card IMG [marker] builds a 256 MiB GPT card with two FAT32 volumes
# (BOOT, CONFIG) holding a few thousand text files for Spotlight to chew on.
make_card() {
	local img=$1 marker=${2:-} disk mps b c
	mkfile -n 256m "$img"
	disk=$(attach "$img" -nomount)
	sudo diskutil partitionDisk "$disk" GPT FAT32 BOOT 128M FAT32 CONFIG R >"$WORK/partition.log" 2>&1
	mps=$(wait_mounted "$disk") || { cat "$WORK/partition.log"; return 1; }
	set -- $mps
	b=$1 c=$2
	cp -R /usr/share/man/man1 "$b/" 2>/dev/null
	mkdir -p "$b/docs" "$c/docs"
	for i in $(seq 1 1500); do
		printf 'WendyOS test document %s\nlorem ipsum dolor sit amet %s\n' "$i" "$RANDOM" >"$b/docs/doc$i.txt"
		printf 'config entry %s\n' "$i" >"$c/docs/cfg$i.txt"
	done
	if [ "$marker" = marker ]; then
		touch "$b/.metadata_never_index" "$c/.metadata_never_index"
	fi
	detach "$disk"
}

enable_spotlight() {
	sudo mdutil -a -i on >"$OUT/mdutil-enable.log" 2>&1
	note "- macOS: $(sw_vers -productVersion) ($(sw_vers -buildVersion))"
	note "- Spotlight on /: $(index_state /)"
}

# index: does a volume setting survive detach + re-attach?
exp_index() {
	local variant disk mps b c
	for variant in control marker mdutil; do
		make_card "$WORK/$variant.img" || { note "- $variant: card build failed"; continue; }
		disk=$(attach "$WORK/$variant.img")
		mps=$(wait_mounted "$disk") || { note "- $variant: volumes never mounted"; continue; }
		set -- $mps
		b=$1 c=$2
		force_index "$b" "$c"
		note "- $variant, first attach (indexing forced on): BOOT '$(index_state "$b")', mount: $(mount | grep " on $b " | sed 's/.*(//')"
		case $variant in
		marker) touch "$b/.metadata_never_index" "$c/.metadata_never_index" ;;
		mdutil) sudo mdutil -i off "$b" "$c" >>"$OUT/index-mdutil.log" 2>&1 ;;
		esac
		sleep 20
		detach "$disk"
		disk=$(attach "$WORK/$variant.img")
		mps=$(wait_mounted "$disk") || { note "- $variant: re-attach never mounted"; continue; }
		set -- $mps
		b=$1 c=$2
		sleep 20
		note "- $variant, re-attach: BOOT '$(index_state "$b")', CONFIG '$(index_state "$c")'," \
			"indexed items in BOOT: $(mdfind -onlyin "$b" -count 'kMDItemFSName == "*"' 2>/dev/null)," \
			".Spotlight-V100 present: $([ -e "$b/.Spotlight-V100" ] && echo yes || echo no)"
		detach "$disk"
	done
}

# repro variants: attach (auto-mount + index), then the CLI's unmount + write.
exp_reflash() {
	local prep=$1 src="$WORK/src.img" img="$WORK/card.img" i delay disk mps rc unmount write prepres t0
	if [ "$prep" = marker ]; then
		make_card "$src" marker || { note "- card build failed"; return; }
	else
		make_card "$src" || { note "- card build failed"; return; }
	fi
	cp "$src" "$img"
	note ""
	note "| # | index delay | prep | unmount | write |"
	note "|---|---|---|---|---|"
	for i in $(seq 1 "$ITERATIONS"); do
		delay=$(( (i - 1) * 5 ))
		disk=$(attach "$img")
		mps=$(wait_mounted "$disk") || { note "| $i | - | - | never mounted | - |"; detach "$disk"; continue; }
		set -- $mps
		[ "$prep" = marker ] || force_index "$1" "$2"
		sleep "$delay"
		prepres=-
		run_bounded 90 "$OUT/unmount-$i.log" sudo -n diskutil unmountDisk "$disk"
		rc=$?
		unmount=$(outcome $rc)
		if [ $rc -ne 0 ]; then
			run_bounded 60 "$OUT/unmount-force-$i.log" sudo -n diskutil unmountDisk force "$disk"
			unmount="$unmount, force $(outcome $?)"
		fi
		t0=$SECONDS
		run_bounded 180 "$OUT/write-$i.log" sudo -n dd if="$src" of="${disk/disk/rdisk}" bs=8m
		rc=$?
		write=$(outcome $rc)
		[ $rc -eq 0 ] && write="ok ($((SECONDS - t0))s)"
		note "| $i | ${delay}s | $prepres | $unmount | $write |"
		detach "$disk"
		rc=$?
		if [ $rc -ne 0 ]; then
			note ""
			note "- detach after iteration $i: $( [ $rc -eq 124 ] && echo "HUNG (driver wedged)" || echo "refused even with -force (rc=$rc)"); stopping here, see detach-$DETACHES.log*"
			break
		fi
	done
}

note "## $MODE"
enable_spotlight
case $MODE in
index) exp_index ;;
repro) exp_reflash none ;;
fix-marker) exp_reflash marker ;;
*) echo "unknown mode $MODE" >&2; exit 2 ;;
esac
cp "$WORK"/*.log "$WORK"/*.hang "$OUT/" 2>/dev/null
exit 0
