# Vidcache — Video Resolution Caching CLI

A Bubble Tea TUI tool that scans `data/cache`, detects video resolutions, validates filenames, and transcodes missing resolution variants into `sizes/` folders.

## FFmpeg Approach Decision

Using **`exec.Command`** to call `ffprobe`/`ffmpeg` directly (not a Go wrapper library):
- Your installed ffmpeg 8.1.2 works great, no CGo headaches
- Progress parsing from ffmpeg's stderr feeds directly into the Bubble Tea progress bar
- Full control over encoding parameters
- Same pattern as your existing imagecache tool

## Data Structure (observed)

```
data/cache/  (symlink → ../../org/data/public/cache)
  24/24-08/13/
    240813_121003-4k.mp4          ← main video (3840×2160)
    sizes/
      240813_121003-1080.mp4      ← transcoded sizes
      240813_121003-720.mp4
      240813_121003-480.mp4
  21/21-06/05/
    210605_112431.mp4             ← portrait (1080×1920), no resolution tag yet
    sizes/
      210605_112431-720.mp4
      210605_112431-480.mp4
```

**Rules observed:**
- Resolution tag (`-4k`, `-2k`, `-1080`, `-720`, `-480`) refers to the **shorter dimension** for landscape, **shorter dimension** for portrait — actually, looking at the data: 3840×2160 → `-4k` (2160p) and portrait 1080×1920 → original is 1080p, sizes go to 720 and 480 referring to the height (larger dim). The tag always refers to the video's "quality tier" based on its shorter side for landscape and its height for portrait.

> [!IMPORTANT]
> **Resolution mapping clarification needed**: Looking at your data, a 3840×2160 landscape video is tagged `-4k`. A 1080×1920 portrait video has a 480-tagged size at 270×480. So the resolution label refers to the **height** of the output, and width scales proportionally. Is this correct? The mapping would be:
> | Tag | Target Height |
> |-----|--------------|
> | `-480` | 480px |
> | `-720` | 720px |
> | `-1080` | 1080px |
> | `-2k` | 1440px |
> | `-4k` | 2160px |

## Open Questions

> [!IMPORTANT]
> **1. What sizes should be generated?** For a 4K main video, should it generate 2k, 1080, 720, and 480? Or only sizes *below* the original resolution? (I'll assume: generate all sizes strictly below the original.)

> [!IMPORTANT]
> **2. Config file format**: Should the config file be YAML, TOML, or a simple key=value `.cfg` file like your imagecache's `paths.cfg`? I'm leaning toward a simple TOML file like:
> ```toml
> # vidcache.toml
> cache_path = "data/cache"
> confirm_before_cache = true
> ```

> [!IMPORTANT]
> **3. Encoding settings**: For the ffmpeg transcoding, what codec preferences do you have? I'll default to:
> - **H.264** (libx264) for broad compatibility
> - **CRF 23** (visually lossless)
> - **Fast preset** for reasonable speed
> - Copy audio stream as-is (`-c:a copy`)

> [!IMPORTANT]
> **4. The `film.mp4` file**: Some files don't follow the `YYMMDD_HHMMSS` naming convention (e.g., `film.mp4`, `dbt-1080.mp4`). Should the rename prompt only add the resolution suffix, or also suggest the date-format rename? I'll assume: only add resolution suffix (e.g., `film.mp4` → `film-4k.mp4`).

## Proposed Changes

### Project Setup

#### [NEW] [go.mod](file:///Users/sacer/code/vidcache/go.mod)
Go module `g.rg-s.com/vidcache` with dependencies:
- `github.com/charmbracelet/bubbletea` — TUI framework
- `github.com/charmbracelet/bubbles` — progress bar, spinner, list components
- `github.com/charmbracelet/lipgloss` — styling
- `github.com/BurntSushi/toml` — config parsing

#### [NEW] [vidcache.toml](file:///Users/sacer/code/vidcache/vidcache.toml)
Default configuration file:
```toml
cache_path = "data/cache"
confirm_before_cache = true
```

---

### Core Logic

#### [NEW] [scanner.go](file:///Users/sacer/code/vidcache/scanner.go)
**File discovery and analysis:**
- Walk `data/cache` recursively, follow symlinks
- Skip `bot` directories and `sizes` directories
- Collect all `.mp4` files
- For each file, call `ffprobe` to get width/height
- Classify as landscape (width > height) or portrait
- Determine the resolution tier from dimensions
- Check if filename already has the correct resolution suffix
- Check which sizes already exist in the `sizes/` subfolder
- Return a structured list of `VideoFile` entries with status (ok / needs-rename / needs-sizes)

#### [NEW] [ffmpeg.go](file:///Users/sacer/code/vidcache/ffmpeg.go)
**FFmpeg integration:**
- `Probe(path) → (width, height, duration, error)` — calls ffprobe via exec
- `Transcode(src, dst, targetHeight, progressCh) → error` — calls ffmpeg with:
  - Scale filter respecting orientation: `-vf "scale=-2:{height}"` for landscape, `-vf "scale={width}:-2"` for portrait
  - H.264/CRF 23/fast preset
  - Parses ffmpeg stderr progress (`frame=`, `time=`) and sends updates to `progressCh`
  - Uses `-progress pipe:1` for machine-readable progress output

#### [NEW] [video.go](file:///Users/sacer/code/vidcache/video.go)
**Data types:**
```go
type Resolution struct {
    Tag    string // "480", "720", "1080", "2k", "4k"
    Height int    // 480, 720, 1080, 1440, 2160
}

type VideoFile struct {
    Path          string
    Dir           string
    Basename      string      // without extension and resolution tag
    CurrentTag    string      // current resolution tag in filename, if any
    ActualRes     Resolution  // detected from ffprobe
    IsPortrait    bool
    Width, Height int
    Duration      float64
    NeedsRename   bool        // filename doesn't match actual resolution
    MissingSizes  []Resolution // sizes that need to be generated
    Status        string      // "ok", "needs-rename", "needs-cache", "queued", "encoding", "done", "error"
}
```

---

### TUI (Bubble Tea)

#### [NEW] [main.go](file:///Users/sacer/code/vidcache/main.go)
Entry point:
- Load config from `vidcache.toml`
- Run the Bubble Tea program

#### [NEW] [tui.go](file:///Users/sacer/code/vidcache/tui.go)
**Bubble Tea model with multiple phases:**

**Phase 1 — Scanning** (spinner):
- Show "Scanning data/cache..." with spinner
- Runs scanner in background

**Phase 2 — Summary + Rename Prompts**:
- Display table of all found videos with their status
- ✅ Files already correctly named with all sizes present
- ⚠️ Files needing rename — prompt Y/N for each
- 📦 Files needing size generation — show queue

**Phase 3 — Conversion Queue**:
- List of files to transcode with their target sizes
- If `confirm_before_cache = true`, prompt Y/N for each main video before its sizes are generated
- Current file: show progress bar with percentage, elapsed time, ETA
- Queue: show remaining files with their sizes
- Completed: show checkmark with file path

**Key bindings:**
- `y` / `n` for confirmations
- `q` / `ctrl+c` to quit (asks confirmation if encoding is in progress)
- `enter` to proceed through phases

**Layout sketch:**
```
╭─ vidcache ──────────────────────────────────────────╮
│                                                     │
│  ✅ 240813_121003-4k.mp4    4K  landscape  [done]   │
│  ✅ 200808_194144-2k.mp4    2K  landscape  [done]   │
│  ⚠️  film.mp4               4K  landscape  [rename?]│
│  📦 240307_212201.mp4       4K  landscape  [queue]  │
│                                                     │
│  ── Current ─────────────────────────────────────── │
│  240307_212201-4k.mp4 → sizes/240307_212201-1080.mp4│
│  ████████████░░░░░░░░░░░░░░░░░  42%  01:23 / 03:15 │
│                                                     │
│  ── Queue (3 remaining) ─────────────────────────── │
│  240307_212201-4k.mp4 → 720, 480                    │
│  film-4k.mp4 → 2k, 1080, 720, 480                  │
│                                                     │
│  [y] confirm  [n] skip  [q] quit                    │
╰─────────────────────────────────────────────────────╯
```

## Workflow

1. **Scan** all mp4 files (excluding bot/, sizes/)
2. **Probe** each file for resolution via ffprobe
3. **Classify** — mark files as ok / needs-rename / needs-sizes
4. **Display** summary: good files shown as ✅, problematic ones highlighted
5. **Rename phase** — for each file with wrong/missing resolution tag, interactively ask
6. **Queue phase** — build queue of missing sizes to generate
7. **Encode phase** — for each queued file (with optional confirmation), transcode and show progress
8. **Done** — show final summary

## Verification Plan

### Manual Verification
- Run the tool and verify it correctly scans and identifies all 58 mp4 files
- Verify it correctly detects landscape vs portrait
- Verify rename prompts work for files like `film.mp4` and `240307_212201.mp4`
- Test encoding a single small file to verify ffmpeg transcoding and progress display
- Verify `confirm_before_cache = false` skips prompts

### Build Verification
- `go build` compiles without errors
- `go vet` passes
