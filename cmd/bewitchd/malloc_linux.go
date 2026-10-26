//go:build linux && cgo

package main

// Cap the number of glibc malloc arenas. DuckDB allocates through glibc, and
// glibc gives each thread that contends for an arena its own (up to 8 per
// core). DuckDB runs one worker thread per core, so on a 16-core host the
// daemon ended up with dozens of arenas; memory freed after each
// archive/compaction stayed fragmented across all of them and RSS ratcheted
// up to ~650 MB, of which DuckDB and the Go heap accounted for ~200 MB.
// malloc_trim doesn't recover it; capping the arenas does.
//
// This runs as an ELF constructor, before the Go runtime starts its threads:
// a Go init() is too late, since by then cgo thread start-up has already
// created several arenas. An explicit MALLOC_ARENA_MAX in the environment wins
// (glibc applies it itself).

/*
#include <malloc.h>
#include <stdlib.h>

__attribute__((constructor)) static void bewitch_cap_malloc_arenas(void) {
	if (getenv("MALLOC_ARENA_MAX") == NULL) {
		mallopt(M_ARENA_MAX, 2);
	}
}
*/
import "C"
