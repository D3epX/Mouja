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
	waveDir   int
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
	return model{loading: true, spinner: s, lat: lat, lon: lon, loc: loc}
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			return m, tea.Batch(m.spinner.Tick, fetchData(m.lat, m.lon))
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

func renderWave(waveFrame int, width int) string {
	if width < 20 {
		return ""
	}
	pat := wavePatterns[waveFrame]
	repeat := width / len(pat)
	var b strings.Builder
	for i := 0; i < repeat; i++ {
		b.WriteString(pat)
	}
	b.WriteString(pat[:width%len(pat)])
	return lipgloss.NewStyle().Foreground(lipgloss.Color(waveColors[waveFrame])).Render(b.String())
}

func renderCard(title string, lines []string, width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#e0f0ff")).Bold(true).Render(title))
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString("  ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return cardStyle.Width(width - 2).Render(b.String())
}

func (m model) View() string {
	if m.err != nil {
		availW := m.width
		if availW < 60 {
			availW = 60
		}
		waveLine := renderWave(m.waveFrame, availW)
		header := headerStyle.Width(availW).Render("🌊  Mouja — " + m.loc)
		errBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ff4444")).
			Padding(1, 2).
			Background(lipgloss.Color("#1a0a0a")).
			Width(availW - 6).
			Render(
				errStyle.Render("✗ Connection Error") + "\n\n" +
					infoStyle.Render(m.err.Error()) + "\n\n" +
					labelStyle.Render("Press [r] to retry  ·  [q] to quit"),
			)
		return lipgloss.JoinVertical(lipgloss.Center,
			header,
			waveLine,
			"",
			errBox,
		)
	}

	if m.loading || m.weather == nil || m.marine == nil {
		availW := m.width
		if availW < 60 {
			availW = 60
		}
		waveLine := renderWave(m.waveFrame, availW)
		header := headerStyle.Width(availW).Render("🌊  Mouja — " + m.loc)
		spinnerView := lipgloss.NewStyle().Foreground(lipgloss.Color("#5dade2")).Render(m.spinner.View())
		loadingBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#1a5276")).
			Padding(1, 2).
			Background(lipgloss.Color("#0a1628")).
			Width(availW - 6).
			Render(spinnerView + "  " + labelStyle.Render("Fetching beach data..."))
		return lipgloss.JoinVertical(lipgloss.Center,
			header,
			waveLine,
			"",
			loadingBox,
		)
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

	availW := m.width
	if availW < 60 {
		availW = 60
	}
	pad := 1
	cardW := (availW - pad*3) / 2

	weatherLines := []string{
		labelStyle.Render("Temperature") + "  " + tempStyle.Render(fmtVal(w.Current.Temp, "°C", 1)),
		labelStyle.Render("Condition") + "    " + valStyle.Render(cond),
		labelStyle.Render("Wind") + "         " + windStyle.Render(fmtVal(w.Current.WindSpeed, " km/h", 1)+" "+wdStr),
		labelStyle.Render("Humidity") + "     " + humidStyle.Render(fmtVal(w.Current.Humidity, "%", 0)),
	}
	weatherCard := renderCard("☀️  Weather", weatherLines, cardW)

	marineLines := []string{
		labelStyle.Render("Water") + "        " + waterStyle.Render(fmtVal(mar.Current.WaterTemp, "°C", 1)),
		labelStyle.Render("Waves") + "        " + waveStyle.Render(fmt.Sprintf("%.2fm %s", mar.Current.WaveHeight, waveDirStr)),
		labelStyle.Render("Wave Period") + "  " + valStyle.Render(fmtVal(mar.Current.WavePeriod, "s", 1)),
		labelStyle.Render("Swell") + "        " + waveStyle.Render(fmt.Sprintf("%.2fm %s", mar.Current.SwellHeight, swellDirStr)),
		labelStyle.Render("Swell Period") + "" + valStyle.Render(fmtVal(mar.Current.SwellPeriod, "s", 1)),
	}
	marineCard := renderCard("🌊  Marine", marineLines, cardW)

	maxT := fmtVal(safeF64(w.Daily.Max, 0), "°C", 1)
	minT := fmtVal(safeF64(w.Daily.Min, 0), "°C", 1)
	prec := fmtVal(safeF64(w.Daily.Prec, 0), "mm", 1)

	forecastLines := []string{
		labelStyle.Render("Max") + "           " + tempStyle.Render(maxT),
		labelStyle.Render("Min") + "           " + tempStyle.Render(minT),
		labelStyle.Render("Rain") + "          " + valStyle.Render(prec),
	}
	forecastCard := renderCard("📅  Forecast", forecastLines, cardW)

	infoLines := []string{
		labelStyle.Render("Location") + "     " + valStyle.Render(m.loc),
		labelStyle.Render("Coords") + "      " + valStyle.Render(fmt.Sprintf("%.2f°N, %.2f°E", m.lat, m.lon)),
		labelStyle.Render("Source") + "      " + valStyle.Render("Open-Meteo"),
		"",
		labelStyle.Render("  [r] refresh  [q] quit"),
	}
	infoCard := renderCard("ℹ️  Info", infoLines, cardW)

	waveLine := renderWave(m.waveFrame, availW)
	header := headerStyle.Width(availW).Render("🌊  Mouja — " + m.loc)

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
