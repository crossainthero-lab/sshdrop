package tui

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crossainthero-lab/sshdrop/internal/config"
	"github.com/crossainthero-lab/sshdrop/internal/connection"
	"github.com/crossainthero-lab/sshdrop/internal/diagnostics"
	"github.com/crossainthero-lab/sshdrop/internal/filesystem"
	"github.com/crossainthero-lab/sshdrop/internal/transfer"
	"github.com/pkg/sftp"
)

type screen int

const (
	deviceScreen screen = iota
	browserScreen
	helpScreen
	inputScreen
	confirmScreen
)

type pane int

const (
	localPane pane = iota
	remotePane
)

type model struct {
	cfg           config.Config
	devices       []config.Device
	deviceCursor  int
	activeDevice  config.Device
	client        *connection.Client
	transfers     *transfer.Manager
	screen        screen
	previous      screen
	activePane    pane
	width, height int
	verbose       bool

	localPath      string
	remotePath     string
	localEntries   []filesystem.Entry
	remoteEntries  []filesystem.Entry
	localCursor    int
	remoteCursor   int
	localSelected  map[string]bool
	remoteSelected map[string]bool

	inputTitle string
	inputValue string
	inputOp    string
	confirmMsg string
	confirmOp  string
	status     string
}

type localListMsg struct {
	path    string
	entries []filesystem.Entry
	err     error
}

type remoteListMsg struct {
	path    string
	entries []filesystem.Entry
	err     error
}

type connectedMsg struct {
	client *connection.Client
	err    error
}

type tickMsg time.Time

var (
	accent     = lipgloss.Color("#a78bfa")
	bg         = lipgloss.Color("#111116")
	fg         = lipgloss.Color("#e5e7eb")
	muted      = lipgloss.Color("#8b8b95")
	errorColor = lipgloss.Color("#fb7185")
	okColor    = lipgloss.Color("#34d399")

	titleStyle      = lipgloss.NewStyle().Foreground(accent).Bold(true)
	paneStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#393341")).Padding(0, 1)
	activePaneStyle = paneStyle.Copy().BorderForeground(accent)
	mutedStyle      = lipgloss.NewStyle().Foreground(muted)
	errStyle        = lipgloss.NewStyle().Foreground(errorColor)
	okStyle         = lipgloss.NewStyle().Foreground(okColor)
)

func Run(cfg config.Config, preselect string, verbose bool) error {
	wd, _ := os.Getwd()
	m := model{
		cfg: cfg, devices: append([]config.Device{}, cfg.Devices...), transfers: transfer.NewManager(),
		screen: deviceScreen, localPath: wd, remotePath: "/", verbose: verbose,
		localSelected: map[string]bool{}, remoteSelected: map[string]bool{},
	}
	sort.SliceStable(m.devices, func(i, j int) bool { return strings.ToLower(m.devices[i].Name) < strings.ToLower(m.devices[j].Name) })
	for i, d := range m.devices {
		if strings.EqualFold(d.Name, preselect) {
			m.deviceCursor = i
			break
		}
	}
	if preselect != "" && len(m.devices) > 0 {
		m.status = "Connecting..."
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{listLocal(m.localPath), tick()}
	if m.status == "Connecting..." {
		cmds = append(cmds, m.connectSelected())
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tick()
	case localListMsg:
		if msg.err != nil {
			m.status = diagnostics.Translate(msg.err)
		} else {
			m.localPath, m.localEntries, m.localCursor = msg.path, msg.entries, 0
		}
	case remoteListMsg:
		if msg.err != nil {
			m.status = diagnostics.Translate(msg.err)
		} else {
			m.remotePath, m.remoteEntries, m.remoteCursor = msg.path, msg.entries, 0
		}
	case connectedMsg:
		if msg.err != nil {
			m.status = diagnostics.Translate(msg.err)
			m.screen = deviceScreen
			return m, nil
		}
		m.client = msg.client
		m.activeDevice = msg.client.Device
		m.screen = browserScreen
		m.status = "Connected"
		if m.activeDevice.DefaultLocalDir != "" {
			m.localPath = m.activeDevice.DefaultLocalDir
		}
		if m.activeDevice.DefaultRemoteDir != "" {
			m.remotePath = m.activeDevice.DefaultRemoteDir
		}
		return m, tea.Batch(listLocal(m.localPath), listRemote(m.client.SFTP, m.remotePath))
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.screen == helpScreen {
		m.screen = m.previous
		return m, nil
	}
	if key == "ctrl+c" || key == "q" {
		if m.client != nil {
			_ = m.client.Close()
		}
		return m, tea.Quit
	}
	if key == "h" {
		m.previous = m.screen
		m.screen = helpScreen
		return m, nil
	}
	switch m.screen {
	case deviceScreen:
		return m.handleDeviceKey(key)
	case browserScreen:
		return m.handleBrowserKey(key)
	case inputScreen:
		return m.handleInputKey(msg)
	case confirmScreen:
		return m.handleConfirmKey(key)
	}
	return m, nil
}

func (m model) handleDeviceKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.deviceCursor > 0 {
			m.deviceCursor--
		}
	case "down", "j":
		if m.deviceCursor < len(m.devices)-1 {
			m.deviceCursor++
		}
	case "enter":
		if len(m.devices) == 0 {
			m.status = "Add a device with: sshdrop device add"
			return m, nil
		}
		m.status = "Connecting..."
		return m, m.connectSelected()
	case "a":
		m.status = "Run sshdrop device add to add a saved device."
	}
	return m, nil
}

func (m model) handleBrowserKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		if m.activePane == localPane {
			m.activePane = remotePane
		} else {
			m.activePane = localPane
		}
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "enter":
		return m.openSelected()
	case "backspace":
		return m.openParent()
	case " ":
		m.toggleSelected()
	case "u":
		m.confirmMsg = "Upload selected local items and overwrite conflicts when confirmed?"
		m.confirmOp = "upload"
		m.screen = confirmScreen
	case "d":
		m.confirmMsg = "Download selected remote items and overwrite conflicts when confirmed?"
		m.confirmOp = "download"
		m.screen = confirmScreen
	case "c":
		m.transfers.CancelActive()
	case "n":
		m.inputTitle, m.inputValue, m.inputOp, m.screen = "New directory name", "", "mkdir", inputScreen
	case "r":
		entry := m.currentEntry()
		if entry == nil || entry.Name == ".." {
			return m, nil
		}
		m.inputTitle, m.inputValue, m.inputOp, m.screen = "Rename to", entry.Name, "rename", inputScreen
	case "x":
		entry := m.currentEntry()
		if entry == nil || entry.Name == ".." {
			return m, nil
		}
		m.confirmMsg = "Delete selected item? Recursive directories require this confirmation."
		m.confirmOp = "delete"
		m.screen = confirmScreen
	}
	return m, nil
}

func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = browserScreen
	case "enter":
		value := strings.TrimSpace(m.inputValue)
		m.screen = browserScreen
		if value == "" {
			return m, nil
		}
		return m.applyInput(value)
	case "backspace":
		if len(m.inputValue) > 0 {
			m.inputValue = m.inputValue[:len(m.inputValue)-1]
		}
	default:
		if len(msg.Runes) > 0 {
			m.inputValue += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(key) {
	case "y":
		m.screen = browserScreen
		return m.applyConfirmed()
	case "n", "esc":
		m.screen = browserScreen
		m.status = "Cancelled"
	}
	return m, nil
}

func (m *model) moveCursor(delta int) {
	if m.activePane == localPane {
		m.localCursor = clamp(m.localCursor+delta, 0, len(m.localEntries)-1)
	} else {
		m.remoteCursor = clamp(m.remoteCursor+delta, 0, len(m.remoteEntries)-1)
	}
}

func (m model) currentEntry() *filesystem.Entry {
	if m.activePane == localPane {
		if m.localCursor >= 0 && m.localCursor < len(m.localEntries) {
			return &m.localEntries[m.localCursor]
		}
		return nil
	}
	if m.remoteCursor >= 0 && m.remoteCursor < len(m.remoteEntries) {
		return &m.remoteEntries[m.remoteCursor]
	}
	return nil
}

func (m *model) toggleSelected() {
	entry := m.currentEntry()
	if entry == nil || entry.Name == ".." {
		return
	}
	selected := m.localSelected
	if m.activePane == remotePane {
		selected = m.remoteSelected
	}
	if selected[entry.Path] {
		delete(selected, entry.Path)
	} else {
		selected[entry.Path] = true
	}
}

func (m model) openSelected() (tea.Model, tea.Cmd) {
	entry := m.currentEntry()
	if entry == nil || !entry.IsDir {
		return m, nil
	}
	if m.activePane == localPane {
		return m, listLocal(entry.Path)
	}
	return m, listRemote(m.client.SFTP, entry.Path)
}

func (m model) openParent() (tea.Model, tea.Cmd) {
	if m.activePane == localPane {
		return m, listLocal(filepath.Dir(m.localPath))
	}
	return m, listRemote(m.client.SFTP, path.Dir(m.remotePath))
}

func (m model) applyConfirmed() (tea.Model, tea.Cmd) {
	switch m.confirmOp {
	case "upload":
		paths := keys(m.localSelected)
		if len(paths) == 0 {
			if e := m.currentEntry(); e != nil && m.activePane == localPane && e.Name != ".." {
				paths = []string{e.Path}
			}
		}
		if len(paths) == 0 {
			m.status = "Select local files or folders first."
			return m, nil
		}
		_, err := m.transfers.EnqueueUpload(context.Background(), m.client.SFTP, paths, m.remotePath)
		if err != nil {
			m.status = diagnostics.Translate(err)
		} else {
			m.localSelected = map[string]bool{}
			m.status = "Upload queued"
		}
		return m, listRemote(m.client.SFTP, m.remotePath)
	case "download":
		paths := keys(m.remoteSelected)
		if len(paths) == 0 {
			if e := m.currentEntry(); e != nil && m.activePane == remotePane && e.Name != ".." {
				paths = []string{e.Path}
			}
		}
		if len(paths) == 0 {
			m.status = "Select remote files or folders first."
			return m, nil
		}
		_, err := m.transfers.EnqueueDownload(context.Background(), m.client.SFTP, paths, m.localPath)
		if err != nil {
			m.status = diagnostics.Translate(err)
		} else {
			m.remoteSelected = map[string]bool{}
			m.status = "Download queued"
		}
		return m, listLocal(m.localPath)
	case "delete":
		return m.deleteSelected()
	}
	return m, nil
}

func (m model) applyInput(value string) (tea.Model, tea.Cmd) {
	if m.inputOp == "mkdir" {
		if m.activePane == localPane {
			err := os.Mkdir(filepath.Join(m.localPath, value), 0o755)
			if err != nil {
				m.status = diagnostics.Translate(err)
			}
			return m, listLocal(m.localPath)
		}
		err := m.client.SFTP.Mkdir(path.Join(m.remotePath, value))
		if err != nil {
			m.status = diagnostics.Translate(err)
		}
		return m, listRemote(m.client.SFTP, m.remotePath)
	}
	if m.inputOp == "rename" {
		entry := m.currentEntry()
		if entry == nil {
			return m, nil
		}
		if m.activePane == localPane {
			err := os.Rename(entry.Path, filepath.Join(filepath.Dir(entry.Path), value))
			if err != nil {
				m.status = diagnostics.Translate(err)
			}
			return m, listLocal(m.localPath)
		}
		err := m.client.SFTP.Rename(entry.Path, path.Join(path.Dir(entry.Path), value))
		if err != nil {
			m.status = diagnostics.Translate(err)
		}
		return m, listRemote(m.client.SFTP, m.remotePath)
	}
	return m, nil
}

func (m model) deleteSelected() (tea.Model, tea.Cmd) {
	entry := m.currentEntry()
	if entry == nil {
		return m, nil
	}
	if m.activePane == localPane {
		err := os.RemoveAll(entry.Path)
		if err != nil {
			m.status = diagnostics.Translate(err)
		}
		return m, listLocal(m.localPath)
	}
	err := removeRemoteAll(m.client.SFTP, entry.Path)
	if err != nil {
		m.status = diagnostics.Translate(err)
	}
	return m, listRemote(m.client.SFTP, m.remotePath)
}

func (m model) connectSelected() tea.Cmd {
	d := m.devices[m.deviceCursor]
	return func() tea.Msg {
		c, err := connection.Dial(d, connection.Options{
			Timeout:       15 * time.Second,
			Password:      connection.PromptPassword,
			ConfirmHost:   connection.ConfirmHostInteractive,
			AllowPassword: true,
			Verbose:       m.verbose,
		})
		return connectedMsg{client: c, err: err}
	}
}

func listLocal(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := filesystem.ListLocal(dir)
		return localListMsg{path: dir, entries: entries, err: err}
	}
}

func listRemote(s *sftp.Client, dir string) tea.Cmd {
	return func() tea.Msg {
		infos, err := s.ReadDir(dir)
		if err != nil {
			return remoteListMsg{path: dir, err: err}
		}
		entries := []filesystem.Entry{filesystem.ParentEntry(dir, true)}
		for _, info := range infos {
			entries = append(entries, filesystem.Entry{
				Name: info.Name(), Path: path.Join(dir, info.Name()), Size: info.Size(), IsDir: info.IsDir(),
				Mode: info.Mode(), ModTime: info.ModTime(), TypeName: filesystem.FormatMode(info.Mode()),
			})
		}
		sort.SliceStable(entries[1:], func(i, j int) bool {
			a, b := entries[i+1], entries[j+1]
			if a.IsDir != b.IsDir {
				return a.IsDir
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		})
		return remoteListMsg{path: dir, entries: entries}
	}
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func removeRemoteAll(s *sftp.Client, p string) error {
	info, err := s.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return s.Remove(p)
	}
	var dirs []string
	w := s.Walk(p)
	for w.Step() {
		if err := w.Err(); err != nil {
			return err
		}
		if w.Stat().IsDir() {
			dirs = append(dirs, w.Path())
		} else if err := s.Remove(w.Path()); err != nil {
			return err
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if err := s.RemoveDirectory(d); err != nil {
			return err
		}
	}
	return nil
}

func (m model) View() string {
	if m.width == 0 {
		m.width = 100
	}
	if m.height == 0 {
		m.height = 30
	}
	base := lipgloss.NewStyle().Foreground(fg).Background(bg).Width(m.width).Height(m.height)
	var body string
	switch m.screen {
	case deviceScreen:
		body = m.viewDevices()
	case browserScreen:
		body = m.viewBrowser()
	case helpScreen:
		body = m.viewHelp()
	case inputScreen:
		body = m.viewBrowser() + "\n" + titleStyle.Render(m.inputTitle+": ") + m.inputValue
	case confirmScreen:
		body = m.viewBrowser() + "\n" + errStyle.Render(m.confirmMsg+"  y/n")
	}
	return base.Render(body)
}

func (m model) viewDevices() string {
	lines := []string{titleStyle.Render("SSHDrop"), mutedStyle.Render("Select a saved SSH device. Press Enter to connect, a to add via CLI wizard, h for help, q to quit."), ""}
	if len(m.devices) == 0 {
		lines = append(lines, "No devices found.", "Run: sshdrop device add")
	} else {
		for i, d := range m.devices {
			cursor := "  "
			if i == m.deviceCursor {
				cursor = ">"
			}
			lines = append(lines, fmt.Sprintf("%s %-20s %s@%s:%d", cursor, d.Name, d.User, d.Host, d.Port))
		}
	}
	if m.status != "" {
		lines = append(lines, "", m.status)
	}
	return strings.Join(lines, "\n")
}

func (m model) viewBrowser() string {
	top := fmt.Sprintf("%s  %s  %s", titleStyle.Render("SSHDrop"), okStyle.Render(m.activeDevice.Name), mutedStyle.Render(m.status))
	paneWidth := max(30, (m.width-4)/2)
	paneHeight := max(10, m.height-9)
	left := m.renderPane("Local", m.localPath, m.localEntries, m.localCursor, m.localSelected, paneWidth, paneHeight, m.activePane == localPane)
	right := m.renderPane("Remote", m.remotePath, m.remoteEntries, m.remoteCursor, m.remoteSelected, paneWidth, paneHeight, m.activePane == remotePane)
	queue := m.renderQueue()
	help := mutedStyle.Render("↑/↓ move  Enter open  Backspace parent  Tab switch  Space select  u upload  d download  n mkdir  r rename  x delete  c cancel  h help  q quit")
	return strings.Join([]string{top, lipgloss.JoinHorizontal(lipgloss.Top, left, right), queue, help}, "\n")
}

func (m model) renderPane(title, current string, entries []filesystem.Entry, cursor int, selected map[string]bool, width, height int, active bool) string {
	style := paneStyle
	if active {
		style = activePaneStyle
	}
	rows := []string{titleStyle.Render(title) + " " + mutedStyle.Render(current)}
	limit := height - 2
	for i := 0; i < len(entries) && i < limit; i++ {
		e := entries[i]
		mark := " "
		if selected[e.Path] {
			mark = "*"
		}
		pointer := " "
		if i == cursor {
			pointer = ">"
		}
		name := e.Name
		if e.IsDir && e.Name != ".." {
			name += "/"
		}
		line := fmt.Sprintf("%s%s %-28.28s %-5s %8s %s", pointer, mark, name, e.TypeName, filesystem.FormatSize(e.Size), e.ModTime.Format("2006-01-02"))
		rows = append(rows, line)
	}
	return style.Width(width).Height(height).Render(strings.Join(rows, "\n"))
}

func (m model) renderQueue() string {
	s := m.transfers.Snapshot()
	if s.Active == nil && s.Queued == 0 && s.Completed == 0 && s.Failed == 0 {
		return mutedStyle.Render("Transfer queue: idle")
	}
	if s.Active == nil {
		return fmt.Sprintf("Transfer queue: queued=%d completed=%d failed=%d", s.Queued, s.Completed, s.Failed)
	}
	pct := 0.0
	if s.Active.Size > 0 {
		pct = float64(s.Active.Transferred) / float64(s.Active.Size)
	}
	bar := progressBar(pct, 24)
	return fmt.Sprintf("Transfer queue: %s %s %s %.0f%% %s/%s %s/s queued=%d completed=%d failed=%d",
		s.Active.Direction, filepath.Base(s.Active.Source), bar, pct*100,
		filesystem.FormatSize(s.Active.Transferred), filesystem.FormatSize(s.Active.Size), filesystem.FormatSize(int64(s.Speed)), s.Queued, s.Completed, s.Failed)
}

func (m model) viewHelp() string {
	return titleStyle.Render("Keyboard Help") + `

↑ / ↓        Move selection
Enter        Open directory
Backspace    Parent directory
Tab          Switch between local and remote panes
Space        Select or deselect item
u            Upload selected local files
d            Download selected remote files
n            Create directory
r            Rename file or directory
x            Delete file or directory
c            Cancel active transfer
h            Show keyboard help
q            Quit

Press any key to return.`
}

func progressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
