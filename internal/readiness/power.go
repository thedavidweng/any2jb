package readiness

import "strings"

func parsePowerSource(text string) string {
	const prefix = "Now drawing from '"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		source := strings.TrimPrefix(line, prefix)
		if !strings.HasSuffix(source, "'") {
			return "unknown"
		}
		source = strings.TrimSuffix(source, "'")
		switch source {
		case "AC Power":
			return "ac"
		case "Battery Power":
			return "battery"
		default:
			return "unknown"
		}
	}
	return "unknown"
}
