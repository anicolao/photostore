# Event Log Position Design

Photostore projections must follow event log order. Wall-clock timestamps and
event IDs are metadata, not replay cursors.

## Event Log

The durable event log is `events/events.jsonl`. Each event is one JSON object
followed by `\n`.

Records are variable length. Replay does not depend on fixed-size records,
padding, timestamp ordering, or UUID ordering.

## Projection Cursor

Each projection records the next byte offset to replay:

```sql
create table projection_state (
  projection_name text primary key,
  log_path text,
  next_offset integer,
  event_id text,
  recorded_at_ms integer
);
```

For the MVP there is one projection named `main`.

`next_offset` is the byte offset where the next unreduced event starts. If the
last projected event ends at byte `123456`, including its trailing newline, then
`next_offset = 123456`.

`event_id` and `recorded_at_ms` are diagnostic fields for the last event that
advanced the cursor.

## Startup Replay

On startup:

1. Open the projection database.
2. Read `projection_state.next_offset` for `main`.
3. If the cursor is absent, use offset `0`. This is the one-time migration path
   for stores created before offset cursoring.
4. Open `events/events.jsonl`.
5. Seek to `next_offset`.
6. Read complete newline-terminated records forward.
7. Apply each event and advance `next_offset` to the byte after that event line
   in the same SQLite transaction as the reducer writes.

If the log ends with a partial non-newline-terminated record, replay ignores
that incomplete tail and does not advance the cursor through it.

## Idempotence

`events_applied(event_id primary key, event_type, recorded_at_ms)` remains an
idempotence guard, not the replay cursor.

During replay:

- if the event ID is absent from `events_applied`, reducers run and the event ID
  is inserted
- if the event ID is already present, reducers are skipped
- in both cases, `projection_state.next_offset` advances in the same transaction

This permits safe one-time full replay during migration and protects manual
recovery cases where a cursor is moved backward.

## Append Protocol

Normal event writes are:

1. Serialize writes within the process.
2. Append one JSON line to `events/events.jsonl`.
3. `fsync` the event log file.
4. Stat the event log to get the new EOF.
5. Apply the event to projections.
6. In the projection transaction, advance `projection_state.next_offset` to the
   new EOF.

Crash outcomes:

- crash before append: no event exists
- crash after append but before projection commit: cursor still points before
  the event, so startup replays it
- crash during projection: SQLite rolls back reducer writes and cursor movement
- crash after projection commit: cursor has advanced, so startup does not replay
  that event

## Future Rotation

If event log rotation is added, `projection_state` must identify both the log
segment and byte offset. Until then, `log_path = events/events.jsonl`.
