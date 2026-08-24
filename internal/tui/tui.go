package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Aayush9029/OmaPad/internal/ipc"
	"github.com/Aayush9029/OmaPad/internal/walkingpad"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	cyan       = lipgloss.Color("#67E8F9")
	blue       = lipgloss.Color("#60A5FA")
	teal       = lipgloss.Color("#2DD4BF")
	dim        = lipgloss.Color("#7A8499")
	red        = lipgloss.Color("#FB7185")
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3B4252")).Padding(1, 2)
)

type stateMessage struct {
	snapshot walkingpad.Snapshot
	err      error
}
type actionMessage struct{ err error }
type tickMessage time.Time

type Model struct {
	client   *ipc.Client
	snapshot walkingpad.Snapshot
	err      error
	busy     bool
	width    int
	height   int
}

func Run(client *ipc.Client) error {
	program := tea.NewProgram(Model{client: client}, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), tick())
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case tea.KeyMsg:
		switch message.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.fetch()
		case " ":
			if !m.busy && m.snapshot.ConnectionState == walkingpad.Ready {
				m.busy = true
				return m, m.command(walkingpad.CommandRequest{Command: walkingpad.CommandStartPause})
			}
		case "s":
			if !m.busy && m.snapshot.ConnectionState == walkingpad.Ready {
				m.busy = true
				return m, m.command(walkingpad.CommandRequest{Command: walkingpad.CommandStop})
			}
		case "left", "h", "down", "j":
			if !m.busy {
				return m.adjustSpeed(-0.5)
			}
		case "right", "l", "up", "k":
			if !m.busy {
				return m.adjustSpeed(0.5)
			}
		}
	case stateMessage:
		m.err = message.err
		if message.err == nil {
			m.snapshot = message.snapshot
		}
	case actionMessage:
		m.busy = false
		m.err = message.err
		return m, m.fetch()
	case tickMessage:
		return m, tea.Batch(m.fetch(), tick())
	}
	return m, nil
}

func (m Model) View() string {
	connected := m.snapshot.ConnectionState == walkingpad.Ready
	state := strings.ToUpper(string(m.snapshot.ConnectionState))
	stateColor := dim
	if connected {
		stateColor = teal
	}
	if m.err != nil {
		state = "OFFLINE"
		stateColor = red
	}
	badge := lipgloss.NewStyle().Bold(true).Foreground(stateColor).Render("● " + state)
	title := lipgloss.NewStyle().Bold(true).Foreground(cyan).Render("OMAPAD")
	header := lipgloss.JoinHorizontal(lipgloss.Top, title, strings.Repeat(" ", max(2, 27-lipgloss.Width(title)-lipgloss.Width(badge))), badge)

	speedValue := fmt.Sprintf("%.1f", m.snapshot.Status.Speed)
	if !connected {
		speedValue = "-"
	}
	speed := lipgloss.NewStyle().Bold(true).Foreground(blue).Render(speedValue)
	unit := lipgloss.NewStyle().Foreground(dim).Render(" km/h")
	target := fmt.Sprintf("target %.1f", m.snapshot.TargetSpeed)
	telemetry := lipgloss.JoinHorizontal(lipgloss.Bottom, speed, unit, strings.Repeat(" ", max(2, 27-lipgloss.Width(speed)-lipgloss.Width(unit)-len(target))), lipgloss.NewStyle().Foreground(teal).Render(target))

	stats := fmt.Sprintf("TIME  %s     DIST  %.2f km     STEPS  %d", m.snapshot.DurationText(), m.snapshot.SessionDistance, m.snapshot.SessionSteps)
	stats = lipgloss.NewStyle().Foreground(dim).Render(stats)

	trackWidth := 38
	fraction := math.Max(0, math.Min(1, m.snapshot.Status.Speed/6))
	filled := int(math.Round(float64(trackWidth) * fraction))
	track := lipgloss.NewStyle().Foreground(teal).Render(strings.Repeat("━", filled)) + lipgloss.NewStyle().Foreground(lipgloss.Color("#303744")).Render(strings.Repeat("━", trackWidth-filled))

	controls := lipgloss.NewStyle().Foreground(dim).Render("h/l  speed    space  start/pause    s  stop    r  refresh    q  quit")
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", telemetry, track, "", stats, "", controls)
	if m.busy {
		content += "\n" + lipgloss.NewStyle().Foreground(cyan).Render("Applying command…")
	} else if m.err != nil {
		content += "\n" + lipgloss.NewStyle().Foreground(red).Render(shortError(m.err.Error(), 78))
	} else if m.snapshot.Error != nil {
		content += "\n" + lipgloss.NewStyle().Foreground(red).Render(shortError(*m.snapshot.Error, 78))
	}
	panel := panelStyle.Render(content)
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	}
	return panel
}

func (m Model) adjustSpeed(delta float64) (tea.Model, tea.Cmd) {
	if m.snapshot.ConnectionState != walkingpad.Ready {
		return m, nil
	}
	speed := math.Max(0.5, math.Min(6, m.snapshot.TargetSpeed+delta))
	if speed == m.snapshot.TargetSpeed {
		return m, nil
	}
	m.busy = true
	m.snapshot.TargetSpeed = speed
	return m, m.command(walkingpad.CommandRequest{Command: walkingpad.CommandSetSpeed, Speed: &speed})
}

func (m Model) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		snapshot, err := m.client.State(ctx)
		return stateMessage{snapshot: snapshot, err: err}
	}
}

func (m Model) command(request walkingpad.CommandRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := m.client.Command(ctx, request)
		return actionMessage{err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return tickMessage(now) })
}

func shortError(message string, limit int) string {
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) <= limit {
		return message
	}
	return message[:limit-1] + "…"
}
