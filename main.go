package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var wmoCodes = map[int]string{
	0: "Clear", 1: "Mainly clear", 2: "Partly cloudy", 3: "Overcast",
	45: "Foggy", 48: "Depositing rime fog", 51: "Light drizzle",
	53: "Moderate drizzle", 55: "Dense drizzle",
	61: "Slight rain", 63: "Moderate rain", 65: "Heavy rain",
	71: "Slight snow", 73: "Moderate snow", 75: "Heavy snow",
	80: "Slight showers", 81: "Moderate showers", 82: "Violent showers",
	95: "Thunderstorm", 96: "Thunderstorm w/ hail", 99: "Thunderstorm w/ heavy hail",
}

var wavePatterns = []string{"  ~   ~   ~  ", "   ~   ~   ~ ", "    ~   ~   ~", "   ~   ~   ~ ", "  ~   ~   ~  ", " ~   ~   ~   "}

var waveColors = []string{"#1a5276", "#1f6da0", "#2980b9", "#3498db", "#5dade2", "#85c1e9", "#aed6f1", "#85c1e9"}

// bg is noticeably lighter than pure black so terminals without truecolor
// fall back to "blue" or "dark blue" instead of "black".
var bg = lipgloss.Color("#0f2b4a")

var (
	headerStyle = lipgloss.NewStyle().Background(lipgloss.Color("#0f2b4a")).Foreground(lipgloss.Color("#e0f0ff")).Bold(true).Padding(0, 1)

	cardBase = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#296b9e")).
			Padding(0, 1).
			Background(bg)

	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8ab4d6")).Background(bg)
	descStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6a8a9a")).Background(bg).Italic(true)
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0f0ff")).Background(bg).Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b6b")).Background(lipgloss.Color("#1a0a0a")).Bold(true)
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6a8a9a")).Background(bg)
	spinStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5dade2")).Background(bg)
)

type Weather struct {
	Current struct {
		Temp      float64 `json:"temperature_2m"`
		Humidity  float64 `json:"relative_humidity_2m"`
		WMO       int     `json:"weather_code"`
		WindSpeed float64 `json:"wind_speed_10m"`
		WindDir   float64 `json:"wind_direction_10m"`
	} `json:"current"`
	Daily struct {
		Max  []float64 `json:"temperature_2m_max"`
		Min  []float64 `json:"temperature_2m_min"`
		Prec []float64 `json:"precipitation_sum"`
	} `json:"daily"`
}

type Marine struct {
	Current struct {
		WaveHeight  float64 `json:"wave_height"`
		WaveDir     float64 `json:"wave_direction"`
		WavePeriod  float64 `json:"wave_period"`
		SwellHeight float64 `json:"swell_wave_height"`
		SwellDir    float64 `json:"swell_wave_direction"`
		SwellPeriod float64 `json:"swell_wave_period"`
		WaterTemp   float64 `json:"sea_surface_temperature"`
	} `json:"current"`
}

type waveTickMsg struct{}
type dataLoadedMsg struct{ w *Weather; m *Marine; lat, lon float64 }
type errMsg struct{ err error }

type model struct {
	loading   bool
	weather   *Weather
	marine    *Marine
	err       error
	spinner   spinner.Model
	width     int
	height    int
	lat, lon  float64
	loc       string
	waveFrame int
	showInput bool
	input     textinput.Model
}

func safeF64(v []float64, i int) float64 {
	if i < len(v) { return v[i] }
	return 0
}

func initialModel(lat, lon float64, loc string) model {
	s := spinner.New()
	s.Style = spinStyle
	ti := textinput.New()
	ti.Placeholder = "36.8, 2.92, Beach Name"
	ti.Prompt = "📍 "
	ti.CharLimit = 60
	ti.Width = 40
	return model{loading: true, spinner: s, lat: lat, lon: lon, loc: loc, input: ti}
}

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return waveTickMsg{} })
}

func fetchWeather(lat, lon float64) (*Weather, error) {
	r, e := http.Get(fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m&daily=temperature_2m_max,temperature_2m_min,precipitation_sum&timezone=auto&forecast_days=1",
		lat, lon))
	if e != nil { return nil, fmt.Errorf("network error: %w", e) }
	defer r.Body.Close()
	if r.StatusCode != 200 { return nil, fmt.Errorf("API status %d", r.StatusCode) }
	b, _ := io.ReadAll(r.Body)
	var w Weather
	return &w, json.Unmarshal(b, &w)
}

func fetchMarine(lat, lon float64) (*Marine, error) {
	r, e := http.Get(fmt.Sprintf(
		"https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&current=wave_height,wave_direction,wave_period,swell_wave_height,swell_wave_direction,swell_wave_period,sea_surface_temperature&timezone=auto&forecast_days=1",
		lat, lon))
	if e != nil { return nil, fmt.Errorf("network error: %w", e) }
	defer r.Body.Close()
	if r.StatusCode != 200 { return nil, fmt.Errorf("API status %d", r.StatusCode) }
	b, _ := io.ReadAll(r.Body)
	var m Marine
	return &m, json.Unmarshal(b, &m)
}

func fetchData(lat, lon float64) tea.Cmd {
	return func() tea.Msg {
		w, e1 := fetchWeather(lat, lon)
		if e1 != nil { return errMsg{e1} }
		m, e2 := fetchMarine(lat, lon)
		if e2 != nil { return errMsg{e2} }
		return dataLoadedMsg{w, m, lat, lon}
	}
}

func windDir(d float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	return dirs[int((d+11.25)/22.5)%16]
}

func fmtVal(v float64, s string, d int) string {
	if d == 0 { return fmt.Sprintf("%.0f%s", v, s) }
	return fmt.Sprintf("%.1f%s", v, s)
}

func windDesc(s float64) string {
	switch { case s < 5: return "Calm"; case s < 15: return "Light"; case s < 25: return "Moderate"; case s < 35: return "Strong"; default: return "Stormy" }
}

func waterDesc(t float64) string {
	switch { case t < 15: return "Cold"; case t < 22: return "Refreshing"; case t < 28: return "Warm"; default: return "Hot" }
}

func precipDesc(p float64) string {
	switch { case p < 0.1: return "Dry"; case p < 1: return "Light"; case p < 5: return "Rainy"; default: return "Heavy" }
}

func periodDesc(p float64) string {
	switch { case p < 5: return "Choppy"; case p < 8: return "Moderate"; case p < 12: return "Clean"; default: return "Powerful" }
}

func ratingText(waveH, windS, precip float64) (string, string, string) {
	s := 0
	if waveH < 0.5 { s += 3 } else if waveH < 1.0 { s += 2 } else if waveH < 1.5 { s += 1 }
	if windS < 10 { s += 3 } else if windS < 20 { s += 2 } else if windS < 30 { s += 1 }
	if precip < 0.1 { s += 2 } else if precip < 1 { s += 1 }
	switch { case s >= 6: return "Great", "#00ff00", "🏖️"; case s >= 4: return "Okay", "#ffff00", "👍"; default: return "Bad", "#ff0000", "😞" }
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchData(m.lat, m.lon), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.showInput {
			switch msg.String() {
			case "enter":
				v := m.input.Value()
				m.showInput = false
				m.input.SetValue("")
				p := parseLoc(v)
				if len(p) >= 2 {
					m.lat, _ = strconv.ParseFloat(p[0], 64)
					m.lon, _ = strconv.ParseFloat(p[1], 64)
					if len(p) >= 3 { m.loc = strings.Join(p[2:], " ") } else { m.loc = fmt.Sprintf("%.2f, %.2f", m.lat, m.lon) }
					m.loading = true
					m.err = nil
					return m, tea.Batch(m.spinner.Tick, fetchData(m.lat, m.lon))
				}
			case "esc":
				m.showInput = false
				m.input.SetValue("")
				return m, nil
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			return m, tea.Batch(m.spinner.Tick, fetchData(m.lat, m.lon))
		case "l":
			m.showInput = true
			m.input.Focus()
			return m, nil
		}
	case dataLoadedMsg:
		m.loading = false
		m.weather, m.marine = msg.w, msg.m
		m.lat, m.lon = msg.lat, msg.lon
		m.err = nil
		return m, nil
	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case waveTickMsg:
		m.waveFrame = (m.waveFrame + 1) % len(wavePatterns)
		return m, tick()
	}
	return m, nil
}

func parseLoc(s string) []string {
	if strings.Contains(s, ",") {
		p := strings.SplitN(s, ",", 3)
		for i := range p { p[i] = strings.TrimSpace(p[i]) }
		return p
	}
	return strings.Fields(s)
}

func waveLine(frame, width int) string {
	if width < 20 { return "" }
	pat := wavePatterns[frame]
	n := width / len(pat)
	var b strings.Builder
	for i := 0; i < n; i++ { b.WriteString(pat) }
	b.WriteString(pat[:width%len(pat)])
	return lipgloss.NewStyle().Foreground(lipgloss.Color(waveColors[frame])).Background(lipgloss.Color("#0f2b4a")).Render(b.String())
}

// padLines extends slice to n elements by appending empty strings (always returns n elements).
func padLines(lines []string, n int) []string {
	r := make([]string, n)
	copy(r, lines)
	return r
}

// line builds a single card row: label  value  · desc.
// Every character (including spaces) gets the background explicitly.
func line(label, value, desc, color string) string {
	l := labelStyle.Render(label)
	v := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Background(bg).Bold(true).Render(value)
	if desc == "" {
		return l + lipgloss.NewStyle().Background(bg).Render("  ") + v
	}
	return l + lipgloss.NewStyle().Background(bg).Render("  ") + v + lipgloss.NewStyle().Background(bg).Render("  ") + descStyle.Render("· "+desc)
}

// card renders a container with border, title, and padCount data lines.
// Every card gets the SAME number of lines so lipgloss.JoinHorizontal pads nothing.
func card(title string, lines []string, innerW int, padCount int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString(lipgloss.NewStyle().Background(bg).Render("  "))
		b.WriteString(l)
		b.WriteString("\n")
	}
	for i := len(lines); i < padCount; i++ {
		b.WriteString("\n")
	}
	return cardBase.Width(innerW).Render(b.String())
}

func (m model) View() string {
	availW := m.width
	if availW < 70 { availW = 70 }

	head := headerStyle.Width(availW).Render("🌊  Mouja — " + m.loc)
	wave := waveLine(m.waveFrame, availW)

	if m.showInput {
		m.input.Width = availW - 14
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#5dade2")).
			Padding(1, 2).Background(bg).Width(availW - 4).
			Render(titleStyle.Render("📍 Change Location") + "\n\n" + m.input.View() + "\n\n" + infoStyle.Render("Enter: lat, lon, name  ·  Esc to cancel"))
		return lipgloss.JoinVertical(lipgloss.Center, head, wave, "", box)
	}

	if m.err != nil {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#ff4444")).
			Padding(1, 2).Background(lipgloss.Color("#1a0a0a")).Width(availW - 4).
			Render(errStyle.Render("✗ Connection Error") + "\n\n" + infoStyle.Render(m.err.Error()) + "\n\n" + infoStyle.Render("[r] retry  ·  [l] location  ·  [q] quit"))
		return lipgloss.JoinVertical(lipgloss.Center, head, wave, "", box)
	}

	if m.loading || m.weather == nil || m.marine == nil {
		s := spinStyle.Render(m.spinner.View())
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#296b9e")).
			Padding(1, 2).Background(bg).Width(availW - 4).
			Render(s + "  " + infoStyle.Render("Fetching beach data..."))
		return lipgloss.JoinVertical(lipgloss.Center, head, wave, "", box)
	}

	w, mar := m.weather, m.marine

	cond := "Unknown"
	if c, ok := wmoCodes[w.Current.WMO]; ok { cond = c }

	wd := windDir(w.Current.WindDir)
	waveD := windDir(mar.Current.WaveDir)
	swellD := windDir(mar.Current.SwellDir)

	rating, rCol, rEmoji := ratingText(mar.Current.WaveHeight, w.Current.WindSpeed, safeF64(w.Daily.Prec, 0))
	stat := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(rCol)).Background(bg).Render(fmt.Sprintf("%s  %s", rEmoji, rating))

	// Card total width (incl border) = innerW + 2.
	// Two cards + 1-char gap = 2*(innerW+2) + 1 = availW
	// => innerW = (availW - 5) / 2
	innerW := (availW - 5) / 2

	// All cards must have the SAME number of data lines so JoinHorizontal
	// doesn't add unstyled padding rows → fixes both misalignment and black bg.
	dataLines := 6

	wData := padLines([]string{
		line("Temp", fmtVal(w.Current.Temp, "°C", 1), "", "#ff6b6b"),
		line("Cond", cond, "", "#e0f0ff"),
		line("Wind", fmtVal(w.Current.WindSpeed, " km/h", 1)+" "+wd, windDesc(w.Current.WindSpeed), "#aed581"),
		line("Humidity", fmtVal(w.Current.Humidity, "%", 0), "", "#48c9b0"),
	}, dataLines)
	wCard := card("☀️  Weather", wData, innerW, dataLines)

	mData := padLines([]string{
		line("Water", fmtVal(mar.Current.WaterTemp, "°C", 1), waterDesc(mar.Current.WaterTemp), "#4fc3f7"),
		line("Waves", fmt.Sprintf("%.2fm %s", mar.Current.WaveHeight, waveD), "", "#5dade2"),
		line("Period", fmtVal(mar.Current.WavePeriod, "s", 1), periodDesc(mar.Current.WavePeriod), "#e0f0ff"),
		line("Swell", fmt.Sprintf("%.2fm %s", mar.Current.SwellHeight, swellD), "", "#5dade2"),
		line("Swell P.", fmtVal(mar.Current.SwellPeriod, "s", 1), periodDesc(mar.Current.SwellPeriod), "#e0f0ff"),
	}, dataLines)
	mCard := card("🌊  Marine", mData, innerW, dataLines)

	fData := padLines([]string{
		line("Max", fmtVal(safeF64(w.Daily.Max, 0), "°C", 1), "", "#ff6b6b"),
		line("Min", fmtVal(safeF64(w.Daily.Min, 0), "°C", 1), "", "#ff6b6b"),
		line("Rain", fmtVal(safeF64(w.Daily.Prec, 0), "mm", 1), "", "#e0f0ff"),
	}, dataLines)
	fCard := card("📅  Forecast", fData, innerW, dataLines)

	iData := padLines([]string{
		line("Location", m.loc, "", "#e0f0ff"),
		line("Coords", fmt.Sprintf("%.2fN %.2fE", m.lat, m.lon), "", "#e0f0ff"),
		"",
		infoStyle.Render("[r] refresh  ·  [l] location  ·  [q] quit"),
	}, dataLines)
	iCard := card("ℹ️  Controls", iData, innerW, dataLines)

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, wCard, " ", mCard)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, fCard, " ", iCard)

	body := lipgloss.JoinVertical(lipgloss.Center, "", stat, "", row1, "", row2)
	return lipgloss.JoinVertical(lipgloss.Center, head, wave, body)
}

func main() {
	lat, lon := 36.80, 2.92
	loc := "Aïn Bénian, Algeria"
	if len(os.Args) >= 3 {
		if v, e := strconv.ParseFloat(os.Args[1], 64); e == nil { lat = v }
		if v, e := strconv.ParseFloat(os.Args[2], 64); e == nil { lon = v }
		if len(os.Args) >= 4 { loc = strings.Join(os.Args[3:], " ") } else { loc = fmt.Sprintf("%.2f, %.2f", lat, lon) }
	}
	p := tea.NewProgram(initialModel(lat, lon, loc), tea.WithAltScreen())
	if _, e := p.Run(); e != nil { panic(e) }
}
