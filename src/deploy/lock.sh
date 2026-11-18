# shellcheck shell=sh
# One process at a time: take_lock <dir> [forever] waits until it holds the lock <dir>, release_lock
# lets it go. It waits up to lock_wait_s seconds; with "forever", as long as the lock's owner runs
# (a start waits for a start that builds for many minutes). Sourced by build.sh and ap; the caller
# defines say and die, as both already do, and its cleanup removes "$locked" if it stops while
# holding a lock. POSIX sh, with `local`, which dash and bash both support.
#
# A lock is a directory, made atomically by mkdir, holding the owner's process ID. A lock without an
# owner yet is being taken, so it is waited for. A lock whose owner is no longer running (a build
# that was killed) is stale: one waiter reclaims it by renaming it away, which only one can do, and
# checks that what it renamed is still the dead owner's lock; if another waiter took the lock in
# between, it is put back. Limit: a process ID reused by an unrelated process keeps a stale lock
# alive until the wait ends, and the error then names the lock to remove. One lock is held at a
# time.
lock_wait_s=120
locked=''
take_lock() {
  local lock=$1 forever=${2:-} i=0 owner moved
  until mkdir -- "$lock" 2>/dev/null; do
    owner=$(cat -- "$lock/pid" 2>/dev/null) || owner=''
    if [ -n "$owner" ] && ! kill -0 "$owner" 2>/dev/null; then
      moved=$lock.stale.$$
      if mv -- "$lock" "$moved" 2>/dev/null; then
        if [ "$(cat -- "$moved/pid" 2>/dev/null)" = "$owner" ]; then
          say "removed the stale lock $lock of process $owner, which is no longer running"
          rm -rf -- "$moved"
        elif [ ! -e "$lock" ]; then
          mv -- "$moved" "$lock" 2>/dev/null || rm -rf -- "$moved"
        else
          rm -rf -- "$moved"
        fi
      fi
      continue
    fi
    if [ -z "$forever" ] || [ -z "$owner" ]; then
      i=$((i + 1))
      [ "$i" -le $((lock_wait_s * 2)) ] ||
        die 1 "another process has held $lock for $lock_wait_s seconds; if none is running, remove it"
    fi
    sleep 0.5
  done
  locked=$lock
  printf '%s\n' "$$" >"$lock/pid"
}
release_lock() { rm -rf -- "$locked"; locked=''; }
