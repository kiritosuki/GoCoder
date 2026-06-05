package tui

import "github.com/charmbracelet/lipgloss"

var (
	UserMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Bold(true)

	SystemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	ToolExecutingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("11")).
				Italic(true)

	ToolCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2"))

	PermissionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	SpinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	InputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))
)
