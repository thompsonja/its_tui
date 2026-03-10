package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds the full color palette for one visual preset.
type Theme struct {
	Name          string
	Focused       lipgloss.Color // active panel border + title
	Muted         lipgloss.Color // inactive panel border + separator
	Title         lipgloss.Color // inactive title text
	Help          lipgloss.Color // help / secondary text
	Bar           lipgloss.Color // top bar background
	BarText       lipgloss.Color // top bar foreground
	HighlightText lipgloss.Color // text drawn on top of Focused background (must contrast)
}

// presets is the ordered list of available themes.
var presets = []Theme{
	{
		Name:          "dark",
		Focused:       lipgloss.Color("86"),  // cyan-green
		Muted:         lipgloss.Color("240"), // dim grey
		Title:         lipgloss.Color("252"), // near-white
		Help:          lipgloss.Color("245"), // mid grey
		Bar:           lipgloss.Color("235"), // very dark
		BarText:       lipgloss.Color("252"), // near-white
		HighlightText: lipgloss.Color("232"), // near-black — readable on cyan-green
	},
	{
		Name:          "light",
		Focused:       lipgloss.Color("23"),  // dark teal
		Muted:         lipgloss.Color("244"), // medium grey
		Title:         lipgloss.Color("237"), // dark grey
		Help:          lipgloss.Color("241"), // medium-dark grey
		Bar:           lipgloss.Color("253"), // light grey
		BarText:       lipgloss.Color("237"), // dark text
		HighlightText: lipgloss.Color("255"), // white — readable on dark teal
	},
	{
		Name:          "dracula",
		Focused:       lipgloss.Color("212"), // pink
		Muted:         lipgloss.Color("61"),  // muted purple (comment)
		Title:         lipgloss.Color("255"), // foreground
		Help:          lipgloss.Color("102"), // soft grey-purple
		Bar:           lipgloss.Color("236"), // dark background
		BarText:       lipgloss.Color("255"), // foreground
		HighlightText: lipgloss.Color("17"),  // very dark — readable on pink
	},
	{
		Name:          "catppuccin",
		Focused:       lipgloss.Color("111"), // blue (Sapphire)
		Muted:         lipgloss.Color("61"),  // overlay
		Title:         lipgloss.Color("189"), // text
		Help:          lipgloss.Color("146"), // subtext
		Bar:           lipgloss.Color("234"), // base
		BarText:       lipgloss.Color("189"), // text
		HighlightText: lipgloss.Color("17"),  // very dark — readable on blue
	},
}

// currentTheme is the active theme; modified only from Update (single goroutine).
var currentTheme = presets[0]

// themeNames returns a comma-separated list of preset names.
func themeNames() string {
	s := ""
	for i, t := range presets {
		if i > 0 {
			s += ", "
		}
		s += t.Name
	}
	return s
}
