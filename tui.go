package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// program is the running tea.Program, used to send progress messages
// from the encoding goroutine.
var program *tea.Program

// ── Phases ──────────────────────────────────────────

type phase int

const (
	phaseScanning phase = iota
	phaseSummary
	phaseRename
	phaseConfirmCache
	phaseEncoding
	phaseDone
)

// ── Messages ────────────────────────────────────────

type scanProgressMsg string
type scanDoneMsg struct{ result *ScanResult }
type scanErrorMsg struct{ err error }

type renameResultMsg struct {
	video *VideoFile
	err   error
}

type encodeStartMsg struct{}
type encodeProgressMsg struct{ progress EncodeProgress }
type encodeDoneMsg struct {
	video *VideoFile
	res   Resolution
	err   error
}
type encodeAllDoneMsg struct{}

// ── Styles ──────────────────────────────────────────

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB347"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))
	queueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	existTagStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#FFFFFF")).
		MarginRight(1)

	missingTagStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Background(lipgloss.Color("#333333")).
		MarginRight(1)
	boldStyle    = lipgloss.NewStyle().Bold(true)
	activeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Underline(true)
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

// ── Model ───────────────────────────────────────────

type model struct {
	cfg     Config
	phase   phase
	width   int
	height  int

	// Scanning phase
	spinner    spinner.Model
	scanStatus string
	scanResult *ScanResult

	// Summary phase
	cursor       int
	selected     map[int]struct{}
	scrollOffset int

	// Rename phase
	renameQueue   []*VideoFile
	renameIdx     int

	// Confirm cache phase
	cacheQueue    []*VideoFile
	confirmIdx    int

	// Encoding phase
	encodeQueue   []encodeJob
	encodeIdx     int
	encodeProgress EncodeProgress
	progressBar   progress.Model
	cancelEncode  context.CancelFunc

	// Error tracking
	errors []string
}

type encodeJob struct {
	video *VideoFile
	res   Resolution
}

func newModel(cfg Config) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(50),
	)

	return model{
		cfg:         cfg,
		phase:       phaseScanning,
		spinner:     s,
		progressBar: p,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startScan(),
	)
}

// ── Update ──────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progressBar.Width = min(msg.Width-10, 60)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.cancelEncode != nil {
				m.cancelEncode()
			}
			return m, tea.Quit
		}

		switch m.phase {
		case phaseSummary:
			return m.updateSummary(msg)
		case phaseRename:
			return m.updateRename(msg)
		case phaseConfirmCache:
			return m.updateConfirmCache(msg)
		case phaseDone:
			if msg.String() == "enter" || msg.String() == "q" {
				return m, tea.Quit
			}
			if msg.String() == "b" || msg.String() == "esc" {
				m.phase = phaseScanning
				m.scanStatus = "Rescanning..."
				return m, m.startScan()
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.progressBar.Update(msg)
		m.progressBar = progressModel.(progress.Model)
		return m, cmd

	case scanProgressMsg:
		m.scanStatus = string(msg)
		return m, nil

	case scanDoneMsg:
		m.scanResult = msg.result
		m.phase = phaseSummary
		m.selected = make(map[int]struct{})
		for i, v := range m.scanResult.Videos {
			if v.Status == StatusNeedsRename || v.Status == StatusNeedsCache {
				m.selected[i] = struct{}{}
			}
		}
		return m, nil

	case scanErrorMsg:
		m.errors = append(m.errors, msg.err.Error())
		m.phase = phaseDone
		return m, nil

	case renameResultMsg:
		if msg.err != nil {
			m.errors = append(m.errors, msg.err.Error())
			msg.video.Status = StatusError
			msg.video.Error = msg.err.Error()
		}
		return m.advanceRename()

	case encodeStartMsg:
		return m, m.processNextEncode()

	case encodeProgressMsg:
		m.encodeProgress = msg.progress
		return m, m.progressBar.SetPercent(msg.progress.Percent)

	case encodeDoneMsg:
		if msg.err != nil {
			errMsg := fmt.Sprintf("%s → %s: %v", msg.video.Filename, msg.res.Tag, msg.err)
			m.errors = append(m.errors, errMsg)
		}
		m.encodeIdx++
		if m.encodeIdx >= len(m.encodeQueue) {
			m.phase = phaseDone
			return m, nil
		}
		return m, m.processNextEncode()

	case encodeAllDoneMsg:
		m.phase = phaseDone
		return m, nil
	}

	return m, nil
}

func (m model) updateSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
		}
	case "down", "j":
		if m.scanResult != nil && m.cursor < len(m.scanResult.Videos)-1 {
			m.cursor++
			maxVisible := m.height - 15
			if maxVisible < 5 {
				maxVisible = 5
			}
			if m.cursor >= m.scrollOffset+maxVisible {
				m.scrollOffset = m.cursor - maxVisible + 1
			}
		}
	case "left", "h":
		if m.scanResult != nil {
			maxVisible := m.height - 15
			if maxVisible < 5 {
				maxVisible = 5
			}
			m.cursor -= maxVisible
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.scrollOffset -= maxVisible
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		}
	case "right", "l":
		if m.scanResult != nil {
			maxVisible := m.height - 15
			if maxVisible < 5 {
				maxVisible = 5
			}
			m.cursor += maxVisible
			if m.cursor >= len(m.scanResult.Videos) {
				m.cursor = len(m.scanResult.Videos) - 1
			}
			m.scrollOffset += maxVisible
			if m.scrollOffset > len(m.scanResult.Videos)-maxVisible {
				m.scrollOffset = len(m.scanResult.Videos) - maxVisible
			}
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		}
	case " ", "space":
		if m.scanResult != nil && len(m.scanResult.Videos) > 0 {
			v := m.scanResult.Videos[m.cursor]
			if v.Status == StatusNeedsRename || v.Status == StatusNeedsCache {
				if _, ok := m.selected[m.cursor]; ok {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = struct{}{}
				}
			}
		}
	case "a":
		if m.scanResult != nil {
			allSelected := true
			for i, v := range m.scanResult.Videos {
				if v.Status == StatusNeedsRename || v.Status == StatusNeedsCache {
					if _, ok := m.selected[i]; !ok {
						allSelected = false
						break
					}
				}
			}
			for i, v := range m.scanResult.Videos {
				if v.Status == StatusNeedsRename || v.Status == StatusNeedsCache {
					if allSelected {
						delete(m.selected, i)
					} else {
						m.selected[i] = struct{}{}
					}
				}
			}
		}
	case "enter":
		if m.scanResult == nil {
			return m, nil
		}
		// Build rename queue.
		m.renameQueue = nil
		for i, v := range m.scanResult.Videos {
			if v.Status == StatusNeedsRename {
				if _, ok := m.selected[i]; ok {
					m.renameQueue = append(m.renameQueue, v)
				}
			}
		}
		if len(m.renameQueue) > 0 {
			m.phase = phaseRename
			m.renameIdx = 0
		} else {
			return m.startCachePhase()
		}
	}
	return m, nil
}

func (m model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.renameIdx >= len(m.renameQueue) {
		return m.startCachePhase()
	}

	video := m.renameQueue[m.renameIdx]
	switch msg.String() {
	case "b", "esc":
		return m.resetToSummary()
	case "y":
		return m, func() tea.Msg {
			err := RenameVideo(video)
			return renameResultMsg{video: video, err: err}
		}
	case "s":
		if len(video.MissingSizes) > 0 {
			video.Status = StatusNeedsCache
		} else {
			video.Status = StatusOK
		}
		return m.advanceRename()
	case "n":
		video.Status = StatusSkipped
		return m.advanceRename()
	}
	return m, nil
}

func (m model) advanceRename() (tea.Model, tea.Cmd) {
	m.renameIdx++
	if m.renameIdx >= len(m.renameQueue) {
		return m.startCachePhase()
	}
	return m, nil
}

func (m model) startCachePhase() (tea.Model, tea.Cmd) {
	// Build cache queue from all videos that need encoding.
	m.cacheQueue = nil
	for i, v := range m.scanResult.Videos {
		if len(v.MissingSizes) > 0 && v.Status != StatusSkipped && v.Status != StatusError {
			if _, ok := m.selected[i]; ok {
				m.cacheQueue = append(m.cacheQueue, v)
			}
		}
	}

	if len(m.cacheQueue) == 0 {
		m.phase = phaseDone
		return m, nil
	}

	if m.cfg.ConfirmBeforeCache {
		m.phase = phaseConfirmCache
		m.confirmIdx = 0
		return m, nil
	}

	// No confirmation needed — start encoding directly.
	return m.startEncoding()
}

func (m model) updateConfirmCache(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmIdx >= len(m.cacheQueue) {
		return m.startEncoding()
	}

	switch msg.String() {
	case "b", "esc":
		return m.resetToSummary()
	case "y":
		m.cacheQueue[m.confirmIdx].Status = StatusQueued
		m.confirmIdx++
		if m.confirmIdx >= len(m.cacheQueue) {
			return m.startEncoding()
		}
	case "n":
		m.cacheQueue[m.confirmIdx].Status = StatusSkipped
		m.confirmIdx++
		if m.confirmIdx >= len(m.cacheQueue) {
			return m.startEncoding()
		}
	case "a":
		// Approve all remaining.
		for i := m.confirmIdx; i < len(m.cacheQueue); i++ {
			m.cacheQueue[i].Status = StatusQueued
		}
		return m.startEncoding()
	}
	return m, nil
}

func (m model) resetToSummary() (tea.Model, tea.Cmd) {
	m.phase = phaseSummary
	if m.scanResult != nil {
		for _, v := range m.scanResult.Videos {
			if v.Status == StatusSkipped {
				if v.NeedsRename {
					v.Status = StatusNeedsRename
				} else if len(v.MissingSizes) > 0 {
					v.Status = StatusNeedsCache
				} else {
					v.Status = StatusOK
				}
			}
		}
	}
	return m, nil
}

func (m model) startEncoding() (tea.Model, tea.Cmd) {
	m.encodeQueue = nil
	for _, v := range m.cacheQueue {
		if v.Status == StatusSkipped {
			continue
		}
		for _, res := range v.MissingSizes {
			m.encodeQueue = append(m.encodeQueue, encodeJob{video: v, res: res})
		}
	}

	if len(m.encodeQueue) == 0 {
		m.phase = phaseDone
		return m, nil
	}

	m.phase = phaseEncoding
	m.encodeIdx = 0
	return m, func() tea.Msg { return encodeStartMsg{} }
}

func (m model) processNextEncode() tea.Cmd {
	if m.encodeIdx >= len(m.encodeQueue) {
		return func() tea.Msg { return encodeAllDoneMsg{} }
	}

	job := m.encodeQueue[m.encodeIdx]
	job.video.Status = StatusEncoding

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelEncode = cancel

	return func() tea.Msg {
		progressCh := make(chan EncodeProgress, 10)
		errCh := make(chan error, 1)

		go func() {
			errCh <- Transcode(
				ctx,
				job.video.Path,
				job.video.SizedPath(job.res),
				job.res.Height,
				job.video.IsPortrait,
				job.video.Duration,
				progressCh,
			)
		}()

		// Forward progress updates to the TUI.
		for p := range progressCh {
			if program != nil {
				program.Send(encodeProgressMsg{progress: p})
			}
		}

		err := <-errCh
		if err == nil {
			job.video.Status = StatusDone
		} else {
			job.video.Status = StatusError
		}
		return encodeDoneMsg{video: job.video, res: job.res, err: err}
	}
}

// ── View ────────────────────────────────────────────

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(" vidcache "))
	b.WriteString("\n\n")

	switch m.phase {
	case phaseScanning:
		b.WriteString(m.viewScanning())
	case phaseSummary:
		b.WriteString(m.viewSummary())
	case phaseRename:
		b.WriteString(m.viewRename())
	case phaseConfirmCache:
		b.WriteString(m.viewConfirmCache())
	case phaseEncoding:
		b.WriteString(m.viewEncoding())
	case phaseDone:
		b.WriteString(m.viewDone())
	}

	if len(m.errors) > 0 {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("Errors:"))
		b.WriteString("\n")
		for _, e := range m.errors {
			b.WriteString(errStyle.Render("  ✗ " + e))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.helpText()))
	b.WriteString("\n")

	return b.String()
}

func (m model) viewScanning() string {
	return fmt.Sprintf("%s Scanning...\n  %s",
		m.spinner.View(),
		dimStyle.Render(m.scanStatus))
}

func (m model) viewSummary() string {
	var b strings.Builder

	r := m.scanResult
	b.WriteString(headerStyle.Render("Scan Results"))
	b.WriteString(fmt.Sprintf("  %d videos found\n\n",
		len(r.Videos)))

	// Show OK files.
	if r.OKCount > 0 {
		b.WriteString(okStyle.Render(fmt.Sprintf("  ✓ %d fully cached", r.OKCount)))
		b.WriteString("\n")
	}
	if r.RenameCount > 0 {
		b.WriteString(warnStyle.Render(fmt.Sprintf("  ⚠ %d need rename", r.RenameCount)))
		b.WriteString("\n")
	}
	if r.CacheCount > 0 {
		b.WriteString(queueStyle.Render(fmt.Sprintf("  ◻ %d need sizes", r.CacheCount)))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	maxVisible := m.height - 15
	if maxVisible < 5 {
		maxVisible = 5
	}
	endIdx := m.scrollOffset + maxVisible
	if endIdx > len(r.Videos) {
		endIdx = len(r.Videos)
	}

	// List each video with status.
	for i := m.scrollOffset; i < endIdx; i++ {
		v := r.Videos[i]
		icon, style := statusIcon(v.Status)
		
		if i == m.cursor {
			style = activeStyle
		}

		orient := "L"
		if v.IsPortrait {
			orient = "P"
		}
		dims := fmt.Sprintf("%dx%d", v.Width, v.Height)

		cursorStr := "  "
		if i == m.cursor {
			cursorStr = "> "
		}

		selStr := "   "
		if v.Status == StatusNeedsRename || v.Status == StatusNeedsCache {
			if _, ok := m.selected[i]; ok {
				selStr = "[x]"
			} else {
				selStr = "[ ]"
			}
		}

		line := fmt.Sprintf("%s%s %s %-40s %4s  %s  %s",
			cursorStr,
			selStr,
			icon,
			truncate(v.Filename, 40),
			v.ActualRes.Tag,
			orient,
			dims,
		)
		b.WriteString(style.Render(line))

		// Show existing sizes.
		if len(v.ExistSizes) > 0 {
			b.WriteString("  ")
			for _, s := range v.ExistSizes {
				b.WriteString(existTagStyle.Render(s.Tag))
			}
		}

		// Show missing sizes.
		if len(v.MissingSizes) > 0 {
			b.WriteString("  ")
			for _, s := range v.MissingSizes {
				b.WriteString(missingTagStyle.Render(s.Tag))
			}
			b.WriteString(dimStyle.Render("(missing)"))
		}

		var extras []string
		if v.HasCaptionEN {
			extras = append(extras, "en:vtt")
		}
		if v.HasCaptionDE {
			extras = append(extras, "de:vtt")
		}
		if v.HasScriptEN {
			extras = append(extras, "en:txt")
		}
		if v.HasScriptDE {
			extras = append(extras, "de:txt")
		}

		if len(extras) > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  [%s]", strings.Join(extras, " "))))
		}

		b.WriteString("\n")
	}

	if len(r.Videos) > maxVisible {
		scrollInfo := fmt.Sprintf("  ... viewing %d-%d of %d ...", m.scrollOffset+1, endIdx, len(r.Videos))
		b.WriteString("\n" + dimStyle.Render(scrollInfo) + "\n")
	}

	return b.String()
}

func (m model) viewRename() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Rename Files"))
	b.WriteString(fmt.Sprintf("  %d/%d\n\n", m.renameIdx+1, len(m.renameQueue)))

	// Show already processed renames.
	for i := 0; i < m.renameIdx && i < len(m.renameQueue); i++ {
		v := m.renameQueue[i]
		origName := v.OriginalFilename

		if v.Status == StatusSkipped {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ⊘ %s (skipped)", origName)))
			b.WriteString("\n")
		} else if v.NeedsRename {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  → %s (kept)", origName)))
			b.WriteString("\n")
		} else {
			b.WriteString(okStyle.Render(fmt.Sprintf("  ✓ %s → %s", origName, v.Filename)))
			b.WriteString("\n")
		}
	}

	// Current rename prompt.
	if m.renameIdx < len(m.renameQueue) {
		v := m.renameQueue[m.renameIdx]
		b.WriteString("\n")
		b.WriteString(activeStyle.Render(fmt.Sprintf("  Rename: %s → %s ?",
			v.Filename, v.CorrectFilename())))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Resolution: %s (%dx%d)\n", v.ActualRes.Tag, v.Width, v.Height))
		b.WriteString(fmt.Sprintf("  Path: %s\n", dimStyle.Render(shortenPath(v.Dir))))
	}

	return b.String()
}

func (m model) viewConfirmCache() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Confirm Encoding"))
	b.WriteString(fmt.Sprintf("  %d/%d\n\n", m.confirmIdx+1, len(m.cacheQueue)))

	// Show already confirmed/skipped.
	for i := 0; i < m.confirmIdx && i < len(m.cacheQueue); i++ {
		v := m.cacheQueue[i]
		if v.Status == StatusSkipped {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ⊘ %s (skipped)", v.Filename)))
			b.WriteString("\n")
		} else {
			tags := make([]string, len(v.MissingSizes))
			for j, s := range v.MissingSizes {
				tags[j] = s.Tag
			}
			b.WriteString(okStyle.Render(fmt.Sprintf("  ✓ %s → %s", v.Filename, strings.Join(tags, ", "))))
			b.WriteString("\n")
		}
	}

	// Current confirmation prompt.
	if m.confirmIdx < len(m.cacheQueue) {
		v := m.cacheQueue[m.confirmIdx]
		tags := make([]string, len(v.MissingSizes))
		for i, s := range v.MissingSizes {
			tags[i] = s.Tag
		}
		b.WriteString("\n")
		b.WriteString(activeStyle.Render(fmt.Sprintf("  Encode %s → sizes: %s ?",
			v.Filename, strings.Join(tags, ", "))))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Resolution: %s (%dx%d) Duration: %.0fs\n",
			v.ActualRes.Tag, v.Width, v.Height, v.Duration))
		b.WriteString(fmt.Sprintf("  Path: %s\n", dimStyle.Render(shortenPath(v.Dir))))
	}

	return b.String()
}

func (m model) viewEncoding() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Encoding"))
	b.WriteString(fmt.Sprintf("  %d/%d jobs\n\n", m.encodeIdx+1, len(m.encodeQueue)))

	// Show completed encodes.
	for i := 0; i < m.encodeIdx && i < len(m.encodeQueue); i++ {
		job := m.encodeQueue[i]
		b.WriteString(okStyle.Render(fmt.Sprintf("  ✓ %s → %s\n",
			job.video.Filename, job.res.Tag)))
	}

	// Current encode.
	if m.encodeIdx < len(m.encodeQueue) {
		job := m.encodeQueue[m.encodeIdx]
		b.WriteString("\n")
		b.WriteString(activeStyle.Render(fmt.Sprintf("  ▶ %s → %s",
			job.video.Filename, job.res.Tag)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s %.0f%%",
			m.progressBar.View(),
			m.encodeProgress.Percent*100))
		if m.encodeProgress.Speed != "" {
			b.WriteString(fmt.Sprintf("  %s", dimStyle.Render(m.encodeProgress.Speed)))
		}
		b.WriteString("\n")
	}

	// Remaining queue.
	remaining := len(m.encodeQueue) - m.encodeIdx - 1
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("\n  %s\n",
			dimStyle.Render(fmt.Sprintf("── Queue (%d remaining) ──", remaining))))
		for i := m.encodeIdx + 1; i < len(m.encodeQueue) && i < m.encodeIdx+6; i++ {
			job := m.encodeQueue[i]
			b.WriteString(queueStyle.Render(fmt.Sprintf("  ◻ %s → %s\n",
				job.video.Filename, job.res.Tag)))
		}
		if remaining > 5 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more\n", remaining-5)))
		}
	}

	return b.String()
}

func (m model) viewDone() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Done"))
	b.WriteString("\n\n")

	if m.scanResult != nil {
		var encoded, skipped, errored int
		for _, v := range m.scanResult.Videos {
			switch v.Status {
			case StatusDone:
				encoded++
			case StatusSkipped:
				skipped++
			case StatusError:
				errored++
			}
		}

		b.WriteString(okStyle.Render(fmt.Sprintf("  ✓ %d fully cached\n", m.scanResult.OKCount+encoded)))
		if skipped > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ⊘ %d skipped\n", skipped)))
		}
		if errored > 0 {
			b.WriteString(errStyle.Render(fmt.Sprintf("  ✗ %d errors\n", errored)))
		}
	}

	return b.String()
}

func (m model) helpText() string {
	switch m.phase {
	case phaseSummary:
		return "[↑/↓] move  [←/→] page  [space] select  [a] toggle all  [enter] proceed  [q] quit"
	case phaseRename:
		return "[y] rename  [s] skip rename  [n] skip entirely  [b/esc] back  [q] quit"
	case phaseConfirmCache:
		return "[y] confirm  [n] skip  [a] approve all  [b/esc] back  [q] quit"
	case phaseEncoding:
		return "[q] cancel & quit"
	case phaseDone:
		return "[b/esc] back to start  [enter/q] quit"
	default:
		return "[q] quit"
	}
}

// ── Helpers ─────────────────────────────────────────

func (m model) startScan() tea.Cmd {
	return func() tea.Msg {
		result, err := ScanVideos(m.cfg.CachePath, func(msg string) {
			// Note: we can't send messages from inside Walk easily
			// without a program reference. The scan is fast enough
			// that we just show the spinner.
		})
		if err != nil {
			return scanErrorMsg{err: err}
		}
		return scanDoneMsg{result: result}
	}
}

func statusIcon(s VideoStatus) (string, lipgloss.Style) {
	switch s {
	case StatusOK, StatusDone:
		return "✓", okStyle
	case StatusNeedsRename:
		return "⚠", warnStyle
	case StatusNeedsCache, StatusQueued:
		return "◻", queueStyle
	case StatusEncoding:
		return "▶", activeStyle
	case StatusError:
		return "✗", errStyle
	case StatusSkipped:
		return "⊘", dimStyle
	default:
		return "?", dimStyle
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func shortenPath(path string) string {
	home, err := filepath.Abs(".")
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil {
		return path
	}
	return rel
}
