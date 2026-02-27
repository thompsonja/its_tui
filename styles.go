package main

import "github.com/charmbracelet/lipgloss"

func panelStyle(focused bool) lipgloss.Style {
	c := currentTheme.Muted
	if focused {
		c = currentTheme.Focused
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c)
}

func titleStyle(focused bool) lipgloss.Style {
	c := currentTheme.Title
	bold := false
	if focused {
		c = currentTheme.Focused
		bold = true
	}
	return lipgloss.NewStyle().
		Foreground(c).
		Bold(bold)
}

// separatorStyle styles the horizontal rule between the viewport and input.
// Must render as exactly 1 line — no borders.
func separatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(currentTheme.Muted)
}

func helpTextStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(currentTheme.Help)
}

// topBarStyle is borderless so Width() sets the exact outer width.
func topBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(currentTheme.Bar).
		Foreground(currentTheme.BarText).
		Bold(true).
		AlignHorizontal(lipgloss.Center).
		Padding(0, 1) // 1-cell left/right padding inside the bar
}
