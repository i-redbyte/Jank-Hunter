# `.jhlog` format version 9

Version 9 is an intentionally incompatible, append-only telemetry container. It is designed for files that can be copied while the Android process is still running, without treating an uncommitted tail as corruption.

## Byte order and bounds

- Fixed-width integers use little endian.
- Variable-width integers use unsigned varint; signed values use zig-zag varint.
- Readers must validate every declared length before allocating.
- A raw chunk is limited to 256 KiB. The normal writer target is 64 KiB.
- A file-header payload is limited to 4 KiB.

## File header

```text
magic                 8 bytes: "JHLOG\r\n" + 0x09
header_payload_length uint32
header_payload_crc32  uint32, IEEE CRC over the payload
header_payload        bytes
```

Header payload schema 1:

```text
schema                         uvarint = 1
required_features              uvarint bit mask
optional_features              uvarint bit mask
run_id                         16 bytes
process_instance_id            16 bytes
session_id                     16 bytes
segment_index                  uvarint
os_pid                         uvarint
collector_start_elapsed_us     uvarint
segment_start_elapsed_us       uvarint
segment_start_unix_ms          uvarint
identity_source                uvarint
process_name_length + UTF-8    bounded string
symbol_namespace_length + data 16-byte stable-symbol contract fingerprint
```

The filename is presentation-only. Run, process, session and segment grouping always use header identity.

The Android writer creates exactly one container for a collection session. Its canonical presentation name is `jh-session-log.YYYY-MM-DD.<index>.jhlog`: the local date is fixed at session start and the decimal `index` is monotonic and never reused. Process identity is intentionally absent from the filename and remains in the header. The writer does not rotate or recover into continuation files. When the effective `maxSessionLogBytes` limit is reached, it best-effort appends a terminal `SIZE_LIMIT` control chunk and seals the same file.

## Committed chunks

Each chunk is independently compressed and becomes readable only after its commit trailer is fully written.

```text
ChunkHeader (32 bytes)
  magic[4]          = "JHC9"
  header_size       uint16 = 32
  flags             uint16
  sequence          uint32, starts at zero
  stored_length     uint32
  raw_length        uint32
  record_count      uint32
  raw_crc32         uint32, IEEE CRC over uncompressed records
  header_crc32      uint32, IEEE CRC over the first 28 header bytes

stored_payload[stored_length]

CommitTrailer (20 bytes)
  magic[4]          = "JHCM"
  sequence          uint32
  stored_length     uint32
  raw_length        uint32
  raw_crc32         uint32
```

Chunk flags select raw or gzip payload and mark the final chunk. The default writer uses balanced gzip compression, not best compression.

A chunk is committed only when header CRC, mirrored trailer fields, decompression, raw length, raw CRC and record count all match. An I/O error poisons the current session file; an event is never blindly replayed after a partial write and the Android writer does not hide the failure by opening a continuation file.

## Record envelope

Every record is length-delimited and therefore forward-skippable:

```text
record_body_length       uvarint
record_type              uvarint
envelope_flags           uvarint
producer_time_delta_us   svarint when HAS_TIME
producer_thread_id       uvarint when HAS_THREAD
context_presence_mask    uvarint when HAS_CONTEXT and not SAME_CONTEXT
context_symbol_refs      SymbolRef values selected by the mask
event_attributes         uvarint when HAS_ATTRIBUTES
record_payload           remaining bytes
```

Producer time is captured before queue admission. Signed deltas are required because multiple producer threads can enqueue out of timestamp order. Time and context state reset at every chunk boundary.

`SAME_CONTEXT` is only a writer-side compression of context already attached atomically to the event. Standalone flow records never act as attribution for subsequent events.

Unknown record types parse the common envelope and skip their remaining payload. Known types parse their required prefix and ignore trailing fields.

## Symbol references

```text
0                 unknown
non-zero even     local dictionary id = token >> 1
1 + fixed uint64  stable build-time symbol id
odd values > 1    reserved
```

Stable IDs are interpreted inside the symbol namespace from the file header. Unresolved stable references remain typed unresolved values; readers must not silently turn them into the same string.

The Gradle plugin derives this global 16-byte namespace from the exact stable-ID algorithm and
owner-map schema contract. Application and library modules deliberately share it because a process
can execute instrumented code from several modules while a `.jhlog` has one header. The plugin writes
the value as exactly 32 lowercase hexadecimal characters to both
`io.jankhunter.symbol_namespace` in the generated Android manifest and `symbolNamespace` in
owner-map metadata. When `--owner-map` is supplied, the CLI requires an exact namespace match with
every input `.jhlog` and rejects incompatible contracts. The namespace is not a source-revision ID:
a stale but contract-compatible map simply leaves newly observed stable IDs unresolved.

## Record types

```text
1  DICTIONARY_DEFINITION
2  SESSION_METADATA
3  DEVICE_CONTEXT
4  HTTP
5  UI_WINDOW
6  STALL
7  MEMORY
8  RETAINED
9  COUNTER
10 GAUGE
11 FLOW_TRANSITION
12 LOG_SPAM
13 PROBLEM
14 RUNTIME_CALL
15 QUALITY_SNAPSHOT
16 SEGMENT_END
```

Types are varints, not a four-bit enum. New types can be added without changing the envelope.

## Quality snapshots

Quality counters are cumulative within a session and never travel through the normal bounded event queue:

```text
snapshot_sequence     uvarint
captured_elapsed_us   uvarint
entry_count           uvarint
entries               repeated(counter_id uvarint, cumulative_value uvarint)
```

The CLI takes the latest snapshot for each run/process/session rather than summing cumulative snapshots. A final clean chunk contains an exact quality snapshot plus `SEGMENT_END`.

Quality IDs distinguish at least queue drops, rejection after close, writer I/O loss, dictionary overflow references, dictionary truncation, metric cardinality loss, invalid metrics, runtime-graph capacity loss, runtime-stack mismatch, log-spam cardinality loss and Handler registry limits.

## File status

- `closed_clean`: a committed FINAL chunk ends exactly at physical EOF.
- `open_clean`: committed chunks end at EOF without FINAL.
- `open_with_tail`: a valid committed prefix is followed by an incomplete next header, payload or trailer. The uncommitted bytes are ignored and reported as an active snapshot, not a data-corruption warning.
- `corrupt`: invalid file-header CRC, an impossible complete chunk header, sequence gap, mismatching complete trailer, decompression/size/CRC/record-count failure, or bytes after FINAL.

Missing FINAL alone means `unclosed`; it does not prove either a crash or corruption.
