#!/usr/bin/env bash
#
# server-stats.sh — basic Linux server performance snapshot.
#
# Usage:
#   ./server-stats.sh                # normal run
#   ./server-stats.sh --no-color     # disable ANSI colours
#   ./server-stats.sh --json         # machine-readable output
#
# Works on any Linux distro. No external dependencies beyond coreutils
# and (optionally) `ss`, `last`, `lastb` for the stretch section.

set -o pipefail

# ---------- Config ----------------------------------------------------------

USE_COLOR=1
USE_JSON=0
for arg in "$@"; do
  case "$arg" in
    --no-color) USE_COLOR=0 ;;
    --json)     USE_JSON=1; USE_COLOR=0 ;;
    -h|--help)
      sed -n '2,12p' "$0"
      exit 0
      ;;
  esac
done

# ---------- Colour helpers --------------------------------------------------

if [[ "$USE_COLOR" -eq 1 && -t 1 ]]; then
  BOLD="\033[1m"
  DIM="\033[2m"
  RED="\033[31m"
  GREEN="\033[32m"
  YELLOW="\033[33m"
  BLUE="\033[34m"
  CYAN="\033[36m"
  RESET="\033[0m"
else
  BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; BLUE=""; CYAN=""; RESET=""
fi

section() { printf "\n${BOLD}${BLUE}== %s ==${RESET}\n" "$1"; }
label()   { printf "${DIM}%-22s${RESET}" "$1"; }

# ---------- Utility ---------------------------------------------------------

# Round a floating-point number to N decimal places.
round() {
  local val="$1" places="${2:-1}"
  printf "%.${places}f" "$val" 2>/dev/null || echo "$val"
}

# Compute "used" percentage given used/total, safely handling zero totals.
pct() {
  local used="$1" total="$2"
  if [[ "$total" == "0" || -z "$total" ]]; then
    echo "0.0"
  else
    awk -v u="$used" -v t="$total" 'BEGIN{ printf "%.1f", (u/t)*100 }'
  fi
}

# Colour a percentage by threshold: green <60, yellow <85, red >=85.
colour_pct() {
  local p="$1"
  awk -v p="$p" 'BEGIN{
    if (p+0 >= 85) exit 2;
    if (p+0 >= 60) exit 1;
    exit 0;
  }'
  case $? in
    2) echo -e "${RED}${p}%${RESET}" ;;
    1) echo -e "${YELLOW}${p}%${RESET}" ;;
    *) echo -e "${GREEN}${p}%${RESET}" ;;
  esac
}

# ---------- Human-readable byte formatting ---------------------------------

human_bytes() {
  local bytes="$1"
  awk -v b="$bytes" 'BEGIN{
    split("B KiB MiB GiB TiB PiB", u, " ");
    i = 1;
    while (b >= 1024 && i < 6) { b /= 1024; i++ }
    printf "%.2f %s", b, u[i];
  }'
}

# ---------- Gather data -----------------------------------------------------

read_cpu() {
  # /proc/stat cpu line: user nice system idle iowait irq softirq steal ...
  read -r _ user nice system idle iowait irq softirq steal _ < /proc/stat
  USER_T=${user:-0}; NICE_T=${nice:-0}; SYS_T=${system:-0}
  IDLE_T=${idle:-0}; IO_T=${iowait:-0}; IRQ_T=${irq:-0}
  SIRQ_T=${softirq:-0}; STEAL_T=${steal:-0}

  local prev_total=$(( PREV_USER + PREV_NICE + PREV_SYS + PREV_IDLE + PREV_IO + PREV_IRQ + PREV_SIRQ + PREV_STEAL ))
  local prev_idle=$(( PREV_IDLE + PREV_IO ))

  local total=$(( USER_T + NICE_T + SYS_T + IDLE_T + IO_T + IRQ_T + SIRQ_T + STEAL_T ))
  local idle=$(( IDLE_T + IO_T ))

  local d_total=$(( total - prev_total ))
  local d_idle=$(( idle - prev_idle ))

  if [[ "$d_total" -le 0 ]]; then
    CPU_PCT=0.0
  else
    CPU_PCT=$(awk -v dt="$d_total" -v di="$d_idle" 'BEGIN{ printf "%.1f", (1 - di/dt)*100 }')
  fi

  PREV_USER=$USER_T; PREV_NICE=$NICE_T; PREV_SYS=$SYS_T
  PREV_IDLE=$IDLE_T; PREV_IO=$IO_T; PREV_IRQ=$IRQ_T
  PREV_SIRQ=$SIRQ_T; PREV_STEAL=$STEAL_T
}

# Sample CPU twice, 1 second apart, so the percentage is meaningful.
sample_cpu() {
  PREV_USER=0; PREV_NICE=0; PREV_SYS=0; PREV_IDLE=0
  PREV_IO=0; PREV_IRQ=0; PREV_SIRQ=0; PREV_STEAL=0
  read_cpu >/dev/null 2>&1
  sleep 1
  read_cpu
}

read_memory() {
  local mem_total mem_avail
  mem_total=$(awk '/^MemTotal:/{print $2}' /proc/meminfo)
  # MemAvailable is a better "free" indicator than MemFree (accounts for
  # reclaimable page cache). Falls back to MemFree on very old kernels.
  mem_avail=$(awk '/^MemAvailable:/{print $2}' /proc/meminfo)
  [[ -z "$mem_avail" ]] && mem_avail=$(awk '/^MemFree:/{print $2}' /proc/meminfo)

  MEM_TOTAL_KB=$mem_total
  MEM_AVAIL_KB=$mem_avail
  MEM_USED_KB=$(( mem_total - mem_avail ))
  MEM_PCT=$(pct "$MEM_USED_KB" "$MEM_TOTAL_KB")
}

read_disk() {
  # Aggregate real filesystems (exclude tmpfs, devtmpfs, squashfs, overlay).
  DISK_TOTAL_KB=0
  DISK_USED_KB=0
  while read -r _ size used _ _ _ _; do
    DISK_TOTAL_KB=$(( DISK_TOTAL_KB + size ))
    DISK_USED_KB=$(( DISK_USED_KB + used ))
  done < <(df -kP -x tmpfs -x devtmpfs -x squashfs -x overlay 2>/dev/null | tail -n +2)
  DISK_PCT=$(pct "$DISK_USED_KB" "$DISK_TOTAL_KB")
}

read_uptime_load() {
  if [[ -r /proc/uptime ]]; then
    local up_secs
    up_secs=$(awk '{printf "%d", $1}' /proc/uptime)
    local d=$(( up_secs / 86400 ))
    local h=$(( (up_secs % 86400) / 3600 ))
    local m=$(( (up_secs % 3600) / 60 ))
    UPTIME="${d}d ${h}h ${m}m"
  else
    UPTIME="unknown"
  fi
  LOAD=$(awk '{print $1", "$2", "$3}' /proc/loadavg)
}

read_os() {
  if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_NAME="${PRETTY_NAME:-$NAME $VERSION}"
  else
    OS_NAME=$(uname -s)
  fi
  KERNEL=$(uname -r)
  ARCH=$(uname -m)
  HOST=$(hostname)
}

read_users() {
  LOGGED_IN=$(who 2>/dev/null | wc -l | tr -d ' ')
  FAILED_LOGINS="n/a"
  if [[ -r /var/log/btmp ]] && command -v lastb >/dev/null 2>&1; then
    FAILED_LOGINS=$(lastb -s yesterday 2>/dev/null | wc -l | tr -d ' ')
  fi
}

# ---------- Output ----------------------------------------------------------

print_header() {
  section "Host"
  label "Hostname:";  echo "$HOST"
  label "OS:";        echo "$OS_NAME"
  label "Kernel:";    echo "$KERNEL ($ARCH)"
  label "Uptime:";    echo "$UPTIME"
  label "Load (1/5/15):"; echo "$LOAD"
}

print_cpu() {
  section "CPU"
  label "Cores:";      echo "$(nproc)"
  label "Model:";      awk -F: '/^model name/{print $2; exit}' /proc/cpuinfo | sed 's/^ *//'
  label "Total usage:"; echo "$(colour_pct "$CPU_PCT")"
}

print_memory() {
  section "Memory"
  label "Total:";     echo "$(human_bytes $(( MEM_TOTAL_KB * 1024 )))"
  label "Used:";      echo "$(human_bytes $(( MEM_USED_KB * 1024 ))) ($(colour_pct "$MEM_PCT"))"
  label "Available:"; echo "$(human_bytes $(( MEM_AVAIL_KB * 1024 )))"
}

print_disk() {
  section "Disk (real filesystems)"
  label "Total:"; echo "$(human_bytes $(( DISK_TOTAL_KB * 1024 )))"
  label "Used:";  echo "$(human_bytes $(( DISK_USED_KB * 1024 ))) ($(colour_pct "$DISK_PCT"))"
  label "Free:";  echo "$(human_bytes $(( (DISK_TOTAL_KB - DISK_USED_KB) * 1024 )))"
}

print_top_cpu() {
  section "Top 5 processes by CPU"
  printf "${BOLD}%-8s %-22s %-8s %-8s %s${RESET}\n" "PID" "COMMAND" "CPU%" "MEM%" "USER"
  # Sort by CPU%, skip the ps header row.
  ps -eo pid,comm,%cpu,%mem,user --sort=-%cpu | head -n 6 | tail -n +2 | \
    while read -r pid comm cpu mem user; do
      printf "%-8s %-22.22s %-8s %-8s %s\n" "$pid" "$comm" "$cpu" "$mem" "$user"
    done
}

print_top_mem() {
  section "Top 5 processes by memory"
  printf "${BOLD}%-8s %-22s %-8s %-8s %s${RESET}\n" "PID" "COMMAND" "MEM%" "CPU%" "USER"
  ps -eo pid,comm,%mem,%cpu,user --sort=-%mem | head -n 6 | tail -n +2 | \
    while read -r pid comm mem cpu user; do
      printf "%-8s %-22.22s %-8s %-8s %s\n" "$pid" "$comm" "$mem" "$cpu" "$user"
    done
}

print_users() {
  section "Sessions"
  label "Logged in users:";    echo "$LOGGED_IN"
  label "Failed logins (24h):"; echo "$FAILED_LOGINS"
}

print_json() {
  # Emit a compact JSON document; useful for piping into a dashboard.
  local cpu_cores; cpu_cores=$(nproc)
  local mem_total=$(( MEM_TOTAL_KB * 1024 ))
  local mem_used=$(( MEM_USED_KB * 1024 ))
  local mem_avail=$(( MEM_AVAIL_KB * 1024 ))
  local disk_total=$(( DISK_TOTAL_KB * 1024 ))
  local disk_used=$(( DISK_USED_KB * 1024 ))
  local disk_free=$(( disk_total - disk_used ))

  cat <<EOF
{
  "host": {
    "hostname": "$HOST",
    "os": "$OS_NAME",
    "kernel": "$KERNEL",
    "arch": "$ARCH",
    "uptime": "$UPTIME",
    "load": "$LOAD"
  },
  "cpu": {
    "cores": $cpu_cores,
    "usage_pct": $CPU_PCT
  },
  "memory": {
    "total_bytes": $mem_total,
    "used_bytes": $mem_used,
    "available_bytes": $mem_avail,
    "used_pct": $MEM_PCT
  },
  "disk": {
    "total_bytes": $disk_total,
    "used_bytes": $disk_used,
    "free_bytes": $disk_free,
    "used_pct": $DISK_PCT
  },
  "sessions": {
    "logged_in": $LOGGED_IN
  }
}
EOF
}

# ---------- Main ------------------------------------------------------------

main() {
  read_os
  read_uptime_load
  read_users
  sample_cpu
  read_memory
  read_disk

  if [[ "$USE_JSON" -eq 1 ]]; then
    print_json
    return
  fi

  print_header
  print_cpu
  print_memory
  print_disk
  print_top_cpu
  print_top_mem
  print_users
  printf "\n${DIM}Generated %s${RESET}\n" "$(date '+%Y-%m-%d %H:%M:%S %Z')"
}

main