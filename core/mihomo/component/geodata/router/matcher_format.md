# Matcher cache v2

Matcher caches are immutable, uncompressed files mapped read-only. The iOS app
builds them from geodata; the low-memory Core opens the same files without
allocating the domain trie or IP range arrays on the Go heap. A successful save
also replaces the app's newly built matcher with a mapped matcher.

`OpenDomainMatcher` and `OpenIPMatcher` replace the reader-based v1 APIs. A v1,
truncated, or structurally invalid file is a cache miss and follows the existing
geodata rebuild path. Cache writes remain controlled by `SaveMatcherCache`.
Keyword strings and compiled regular expressions remain on the Go heap.

## Encoding

All integer fields are little-endian. Offsets are relative to their containing
structure. Padding written by the encoder is zero. No Go pointers or runtime
object layouts are serialized.

Both matcher headers are 32 bytes:

| Offset | Domain matcher | IP matcher |
| --- | --- | --- |
| 0 | Eight bytes: `02 4d 49 48 4f 4d 4f 44` | Eight bytes: `02 4d 49 48 4f 4d 4f 49` |
| 8 | Original rule count, uint64 | Original rule count, uint64 |
| 16 | Domain-set byte length, uint64; zero means absent | Merged range count, uint64 |
| 24 | Other matcher count, uint64 | Reserved, zero |

The domain header is followed by the domain set, then the other matchers. Each
other matcher is a uint64 byte length followed by the UTF-8 string returned by
`Matcher.String()` (`keyword:...`, `regexp:...`, etc.). There is no trailing data.

### Domain set

The set starts with `02 44 4f 4d 53 45 54 00`, followed by five pairs of uint64
fields: section offset and section byte length. The 88-byte header describes:

1. Terminal-node bitmap (`uint64` words).
2. Trie topology bitmap (`uint64` words).
3. Edge labels (bytes).
4. Rank index (`int32` values, including the final total).
5. Select index (`int32` positions of every 32nd set bit).

Sections occur in that order, each padded to an eight-byte boundary. The decoder
checks canonical section bounds, tree topology, bitmap padding, and rank/select
values before publishing the matcher. Validation scans the bitmaps and indexes
without rebuilding them. Little-endian hosts borrow aligned numeric arrays
directly; unsupported byte order or alignment is rejected.

The existing domain-set v1 encoding used by rule providers is separate and is
unchanged.

### IP ranges

Each range occupies 40 bytes: one address-family byte (`32` or `128`), seven
reserved bytes, then the inclusive start and end addresses, 16 bytes each in
network byte order. IPv4 uses its IPv4-mapped 16-byte representation; the family
byte distinguishes it from an actual IPv6 address in `::ffff:0:0/96`.

Ranges are sorted and non-overlapping using `netip.Addr.Compare` ordering.
Lookups binary-search the records and decode only the inspected endpoints.

## Ownership and replacement

Each mapped matcher retains its mapping owner and uses `runtime.KeepAlive` after
accessing borrowed memory. The owner's finalizer unmaps the file once no matcher
references it. Clearing the cache must not explicitly close mappings: installed
rules and in-flight lookups may still reference them.

Writes go to a temporary file in the cache directory and publish by rename only
after writing and closing successfully. Never truncate a published cache file.
Existing mappings continue to reference the previous file after replacement or
unlink. Geodata invalidation removes the affected disk caches and resets the
in-memory caches under the same lock that excludes matcher loading/building.

Read-only mappings use `mmap` on Darwin (including iOS) and Linux, and file
mapping on Windows. Other platforms return a cache miss and retain the geodata
fallback. The first uncached build still needs heap memory; this format reduces
the steady-state footprint and subsequent load allocations.
