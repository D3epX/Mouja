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

var wavePatterns = []string{
	"  ~   ~   ~  ",
	"   ~   ~   ~ ",
	"    ~   ~   ~",
	"   ~   ~   ~ ",
	"  ~   ~   ~  ",
	" ~   ~   ~   ",
}

var waveColors = []string{
	"#1a5276", "#1f6da0", "#2980b9", "#3498db",
	"#5dade2", "#85c1e9", "#aed6f1", "#85c1e9",
}

var (
	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0d2137")).
			Foreground(lipgloss.Color("#e0f0ff")).
			Bold(true).Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#1a5276")).
			Padding(0, 1).
			Background(lipgloss.Color("#0a1628"))

	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7fa8c9"))
	valStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0f0ff"))
	tempStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b6b"))
	waterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4fc3f7"))
	waveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5dade2"))
	windStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#aed581"))
	humidStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#48c9b0"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b6b")).Bold(true)
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5d7fa0"))
	descStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5a7a8a")).Italic(true)
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0f0ff")).Bold(true)
)

type Weather struct {
	Current struct {
		Temp       float64 `json:"temperature_2m"`
		Humidity   float64 `json:"relative_humidity_2m"`
		WMO        int     `json:"weather_code"`
		WindSpeed  float64 `json:"wind_speed_10m"`
		WindDir    float64 `json:"wind_direction_10m"`
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
type dataLoadedMsg struct {
	w   *Weather
	m   *Marine
	lat float64
	lon float64
}
type errMsg struct{ err error }

type model struct {
	loading   bool
	weather   *Weather
	marine    *Marine
	err       error
	spinner   spinner.Model
	width     int
	height    int
	lat       float64
	lon       float64
	loc       string
	waveFrame int
	showInput bool
	input     textinput.Model
}

func safeF64(vals []float64, idx int) float64 {
	if idx < len(vals) {
		return vals[idx]
	}
	return 0
}

func initialModel(lat, lon float64, loc string) model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#5dade2"))
	ti := textinput.New()
	ti.Placeholder = "36.8, 2.92, Beach Name"
	ti.Prompt = "📍 "
	ti.CharLimit = 60
	ti.Width = 40
	return model{loading: true, spinner: s, lat: lat, lon: lon, loc: loc, input: ti}
}

func waveTicker() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return waveTickMsg{}
	})
}

func fetchWeather(lat, lon float64) (*Weather, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m&daily=temperature_2m_max,temperature_2m_min,precipitation_sum&timezone=auto&forecast_days=1",
		lat, lon,
	)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}
	var w Weather
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &w, nil
}

func fetchMarine(lat, lon float64) (*Marine, error) {
	url := fmt.Sprintf(
		"https://marine-api.open-meteo.com/v1/marine?latitude=%.2f&longitude=%.2f&current=wave_height,wave_direction,wave_period,swell_wave_height,swell_wave_direction,swell_wave_period,sea_surface_temperature&timezone=auto&forecast_days=1",
		lat, lon,
	)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}
	var m Marine
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &m, nil
}

func fetchData(lat, lon float64) tea.Cmd {
	return func() tea.Msg {
		w, err := fetchWeather(lat, lon)
		if err != nil {
			return errMsg{err}
		}
		m, err := fetchMarine(lat, lon)
		if err != nil {
			return errMsg{err}
		}
		return dataLoadedMsg{w: w, m: m, lat: lat, lon: lon}
	}
}

func windDir(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int((deg+11.25)/22.5) % 16
	return dirs[idx]
}

func fmtVal(v float64, suffix string, decimals int) string {
	if decimals == 0 {
		return fmt.Sprintf("%.0f%s", v, suffix)
	}
	return fmt.Sprintf("%.1f%s", v, suffix)
}

func tempDesc(t float64) string {
	switch {
	case t < 10:
		return "Cold"
	case t < 20:
		return "Mild"
	case t < 30:
		return "Warm"
	default:
		return "Hot"
	}
}

func waveDesc(h float64) string {
	switch {
	case h < 0.3:
		return "Calm"
	case h < 0.6:
		return "Slight"
	case h < 1.0:
		return "Moderate"
	case h < 1.5:
		return "Rough"
	default:
		return "Very rough"
	}
}

func windDesc(s float64) string {
	switch {
	case s < 5:
		return "Calm"
	case s < 15:
		return "Light breeze"
	case s < 25:
		return "Moderate wind"
	case s < 35:
		return "Strong wind"
	default:
		return "Stormy"
	}
}

func waterTempDesc(t float64) string {
	switch {
	case t < 15:
		return "Cold"
	case t < 22:
		return "Refreshing"
	case t < 28:
		return "Warm"
	default:
		return "Hot"
	}
}

func humidDesc(h float64) string {
	switch {
	case h < 40:
		return "Dry"
	case h < 60:
		return "Comfortable"
	case h < 80:
		return "Humid"
	default:
		return "Very humid"
	}
}

func precipDesc(p float64) string {
	if p == 0 {
		return "Dry"
	}
	if p < 1 {
		return "Light rain"
	}
	if p < 5 {
		return "Rainy"
	}
	return "Heavy rain"
}

func periodDesc(p float64) string {
	switch {
	case p < 5:
		return "Choppy"
	case p < 8:
		return "Moderate"
	case p < 12:
		return "Clean"
	default:
		return "Powerful"
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchData(m.lat, m.lon), waveTicker())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.showInput {
			switch msg.String() {
			case "enter":
				val := m.input.Value()
				m.showInput = false
				m.input.SetValue("")
				parts := parseLoc(val)
				if len(parts) >= 2 {
					m.lat, _ = strconv.ParseFloat(parts[0], 64)
					m.lon, _ = strconv.ParseFloat(parts[1], 64)
					if len(parts) >= 3 {
						m.loc = strings.Join(parts[2:], " ")
					} else {
						m.loc = fmt.Sprintf("%.2f, %.2f", m.lat, m.lon)
					}
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
			return m, nil
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
		m.weather = msg.w
		m.marine = msg.m
		m.lat = msg.lat
		m.lon = msg.lon
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
		return m, waveTicker()
	}

	return m, nil
}

func parseLoc(s string) []string {
	if strings.Contains(s, ",") {
		parts := strings.SplitN(s, ",", 3)
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return strings.Fields(s)
}

func ratingText(waveH, windS, precip float64) (string, string, string) {
	score := 0
	if waveH < 0.5 {
		score += 3
	} else if waveH < 1.0 {
		score += 2
	} else if waveH < 1.5 {
		score += 1
	}
	if windS < 10 {
		score += 3
	} else if windS < 20 {
		score += 2
	} else if windS < 30 {
		score += 1
	}
	if precip == 0 {
		score += 2
	} else if precip < 1 {
		score += 1
	}

	switch {
	case score >= 6:
		return "Great", "#00ff00", "🏖️"
	case score >= 4:
		return "Okay", "#ffff00", "👍"
	default:
		return "Bad", "#ff0000", "😞"
	}
}

func renderWave(frame int, width int) string {
	if width < 20 {
		return ""
	}
	pat := wavePatterns[frame]
	repeat := width / len(pat)
	var b strings.Builder
	for i := 0; i < repeat; i++ {
		b.WriteString(pat)
	}
	b.WriteString(pat[:width%len(pat)])
	return lipgloss.NewStyle().Foreground(lipgloss.Color(waveColors[frame])).Render(b.String())
}

func renderCard(title string, lines []string, width int) string {
	var b strings.Builder
	b.WriteString(keyStyle.Render(title))
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return cardStyle.Width(width).Render(b.String())
}

func line(label, value, desc, style string) string {
	l := labelStyle.Render(label)
	v := lipgloss.NewStyle().Foreground(lipgloss.Color(style)).Render(value)
	d := ""
	if desc != "" {
		d = descStyle.Render(" · " + desc)
	}
	if desc != "" {
		return l + " " + v + d
	}
	return l + " " + v
}

func (m model) View() string {
	availW := m.width
	if availW < 60 {
		availW = 60
	}

	waveLine := renderWave(m.waveFrame, availW)
	header := headerStyle.Width(availW).Render("🌊  Mouja — " + m.loc)

	if m.showInput {
		m.input.Width = availW - 10
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5dade2")).
			Padding(1, 2).
			Background(lipgloss.Color("#0a1628")).
			Width(availW - 6).
			Render(
				keyStyle.Render("📍 Change Location")+"\n\n"+
					m.input.View()+"\n\n"+
					infoStyle.Render("Enter: lat, lon, name  ·  Esc to cancel"),
			)
		return lipgloss.JoinVertical(lipgloss.Center, header, waveLine, "", inputBox)
	}

	if m.err != nil {
		errBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ff4444")).
			Padding(1, 2).
			Background(lipgloss.Color("#1a0a0a")).
			Width(availW - 6).
			Render(
				errStyle.Render("✗ Connection Error")+"\n\n"+
					infoStyle.Render(m.err.Error())+"\n\n"+
					labelStyle.Render("[r] retry  ·  [l] change location  ·  [q] quit"),
			)
		return lipgloss.JoinVertical(lipgloss.Center, header, waveLine, "", errBox)
	}

	if m.loading || m.weather == nil || m.marine == nil {
		spinnerView := lipgloss.NewStyle().Foreground(lipgloss.Color("#5dade2")).Render(m.spinner.View())
		loadingBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#1a5276")).
			Padding(1, 2).
			Background(lipgloss.Color("#0a1628")).
			Width(availW - 6).
			Render(spinnerView + "  " + labelStyle.Render("Fetching beach data..."))
		return lipgloss.JoinVertical(lipgloss.Center, header, waveLine, "", loadingBox)
	}

	w := m.weather
	mar := m.marine

	cond := "Unknown"
	if c, ok := wmoCodes[w.Current.WMO]; ok {
		cond = c
	}

	wdStr := windDir(w.Current.WindDir)
	waveDirStr := windDir(mar.Current.WaveDir)
	swellDirStr := windDir(mar.Current.SwellDir)

	rating, ratingColor, ratingEmoji := ratingText(mar.Current.WaveHeight, w.Current.WindSpeed, safeF64(w.Daily.Prec, 0))
	statusLine := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ratingColor)).
		Render(fmt.Sprintf("%s  %s", ratingEmoji, rating))

	pad := 1
	cardContentW := (availW-pad)/2 - 2

	weatherLines := []string{
		line("Temperature", fmtVal(w.Current.Temp, "°C", 1), tempDesc(w.Current.Temp), "#ff6b6b"),
		line("Condition", cond, "", "#e0f0ff"),
		line("Wind", fmtVal(w.Current.WindSpeed, " km/h", 1)+" "+wdStr, windDesc(w.Current.WindSpeed), "#aed581"),
		line("Humidity", fmtVal(w.Current.Humidity, "%", 0), humidDesc(w.Current.Humidity), "#48c9b0"),
	}
	weatherCard := renderCard("☀️  Weather", weatherLines, cardContentW)

	marineLines := []string{
		line("Water", fmtVal(mar.Current.WaterTemp, "°C", 1), waterTempDesc(mar.Current.WaterTemp), "#4fc3f7"),
		line("Waves", fmt.Sprintf("%.2fm %s", mar.Current.WaveHeight, waveDirStr), waveDesc(mar.Current.WaveHeight), "#5dade2"),
		line("Period", fmtVal(mar.Current.WavePeriod, "s", 1), periodDesc(mar.Current.WavePeriod), "#e0f0ff"),
		line("Swell", fmt.Sprintf("%.2fm %s", mar.Current.SwellHeight, swellDirStr), waveDesc(mar.Current.SwellHeight), "#5dade2"),
		line("Swell per.", fmtVal(mar.Current.SwellPeriod, "s", 1), periodDesc(mar.Current.SwellPeriod), "#e0f0ff"),
	}
	marineCard := renderCard("🌊  Marine", marineLines, cardContentW)

	maxT := fmtVal(safeF64(w.Daily.Max, 0), "°C", 1)
	minT := fmtVal(safeF64(w.Daily.Min, 0), "°C", 1)
	prec := fmtVal(safeF64(w.Daily.Prec, 0), "mm", 1)

	forecastLines := []string{
		line("Max", maxT, "", "#ff6b6b"),
		line("Min", minT, "", "#ff6b6b"),
		line("Rain", prec, precipDesc(safeF64(w.Daily.Prec, 0)), "#e0f0ff"),
	}
	forecastCard := renderCard("📅  Forecast", forecastLines, cardContentW)

	infoLines := []string{
		line("Location", m.loc, "", "#e0f0ff"),
		line("Coords", fmt.Sprintf("%.2f°N, %.2f°E", m.lat, m.lon), "", "#e0f0ff"),
		"",
		infoStyle.Render("[r] refresh  [l] location  [q] quit"),
	}
	infoCard := renderCard("ℹ️  Controls", infoLines, cardContentW)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, weatherCard, strings.Repeat(" ", pad), marineCard)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, forecastCard, strings.Repeat(" ", pad), infoCard)

	body := lipgloss.JoinVertical(lipgloss.Center,
		statusLine,
		"",
		topRow,
		"",
		bottomRow,
	)

	return lipgloss.JoinVertical(lipgloss.Center, header, waveLine, body)
}

func main() {
	lat := 36.80
	lon := 2.92
	loc := "Aïn Bénian, Algeria"

	if len(os.Args) >= 3 {
		if v, err := strconv.ParseFloat(os.Args[1], 64); err == nil {
			lat = v
		}
		if v, err := strconv.ParseFloat(os.Args[2], 64); err == nil {
			lon = v
		}
		if len(os.Args) >= 4 {
			loc = strings.Join(os.Args[3:], " ")
		} else {
			loc = fmt.Sprintf("%.2f, %.2f", lat, lon)
		}
	}

	p := tea.NewProgram(initialModel(lat, lon, loc), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
