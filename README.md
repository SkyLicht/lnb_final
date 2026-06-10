# Log Watcher

Go service for watching multiple industrial log directories with `github.com/fsnotify/fsnotify`, parsing detected files, and deleting them after successful processing.

## Project Layout

```txt
cmd/app/main.go              application entrypoint
cmd/watcher/main.go          compatibility entrypoint using default options
internal/config/config.go    JSON configuration loader and validation
internal/watcher/watcher.go  fsnotify directory watchers and event filtering
internal/processor           worker queue, file stabilization, retry, delete/error handling
internal/parser/parser.go    parser dispatcher
internal/parser/functions     parser function packages
internal/logger/logger.go    basic stdout/stderr logger
config.json                  example watcher configuration
.env                         runtime settings
```

## Configuration

`config.json` is an array of watcher definitions:

```json
[
  {
    "name": "machine_01_logs",
    "watcher_type": "on_file_created",
    "file_dir": "C:\\logs\\machine_01",
    "function": "parse_machine_01",
    "output": "C:\\data"
  }
]
```

Supported `watcher_type` values:

```txt
on_file_created
on_file_updated
```

Supported parser functions in the initial dispatcher:

```txt
parse_machine_01
parse_machine_02
default_parser
npm_type1
```

For `npm_type1`, tower events (`EventCode` + `EventDetailCode` = `50-000000`) write machine tower state to:

```txt
<output>\<watcher_name>.json
```

The output document is created when missing and initialized with `machine_1` through `machine_4`. Each tower event updates the matching `machine_<MCNo>` aggregate `tower.state` and the matching stage/lane key, for example `02_01`. `last_update` is generated from the PC clock when the event is processed.

## Run

Create the configured directories first, then run:

```sh
go run ./cmd/app
```

Runtime settings are loaded from `.env`:

```env
CONFIG_PATH=config.json
WORKERS=8
QUEUE_SIZE=2048
STABLE_FOR=750ms
RETRY_DELAY=250ms
MAX_RETRIES=40
SCAN_INTERVAL=30s
METRICS_INTERVAL=1m
```

To use a different env file:

```sh
go run ./cmd/app -env .env.production
```

## Processing Behavior

For each matching fsnotify event, the watcher submits a job to a bounded queue and returns immediately to avoid blocking event ingestion. Worker goroutines then:

1. Validate that the path exists and is not a directory.
2. Wait until file size and modification time remain unchanged for `-stable-for`.
3. Retry reads to handle temporary writer locks.
4. Dispatch to the parser named by the watcher configuration.
5. Delete the file after successful parsing.
6. Move failed files into an `error` folder under the watched directory when possible.

Duplicate processing is prevented with an in-flight path registry. A file already queued or being processed will not be queued again until processing completes.

## Production Run

For a 24-hour production trial, build a binary and run it from the project directory:

```sh
go build -o log-watcher.exe ./cmd/app
.\log-watcher.exe -env .env
```

Production hardening included in the app:

```txt
Startup scan        queues files already present before fsnotify starts delivering new events
Periodic scan       re-queues missed files every SCAN_INTERVAL
Duplicate guard     prevents the same path from being processed concurrently
Worker recovery     logs parser/processing panics as failures without killing the worker loop
Metrics log         emits submitted/processed/failed/duplicate/queue_full counters every METRICS_INTERVAL
Atomic output write writes JSON through a temp file and replacement step
Error traceability  moves failed source files into <file_dir>\error when possible
```

Before the trial, confirm:

```txt
The process account can read/delete files in file_dir.
The process account can create files in output.
The output directory is not inside the watched directory.
The machine writing logs uses final file names, or temp files end in .tmp, .temp, .part, or .crdownload.
```

## Extending Parsers

Add a parser package under `internal/parser/functions`, then register it in `internal/parser/parser.go`:

```go
d.Register("parse_new_machine", parseNewMachine)
```

Parser functions receive the context, file path, and raw file content. Replace the placeholder line-count logic with machine-specific extraction and business logic as needed.
