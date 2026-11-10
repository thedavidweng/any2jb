package session

import (
	"context"
	"errors"
	"slices"
)

func keyboardPrerequisites(checks []Check, selected string) error {
	observed := slices.ContainsFunc(checks, func(check Check) bool {
		return check.ID == selected && check.GatesTransport == "usb-hid" && check.State != RequiresAttention
	})
	if !observed {
		return errors.New("selected keyboard controller was not observed")
	}
	for _, check := range checks {
		if check.GatesTransport == "usb-hid" && check.State == RequiresAttention && check.ID != "usb-controllers" {
			return errors.New("USB keyboard prerequisite requires attention")
		}
	}
	return nil
}

func (c *SessionController) inspectKeyboard(ctx context.Context, run Run, input KeyboardTransport) error {
	checks, err := c.env.Platform.Inspect(ctx, run)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.s.Checks = slices.Clone(checks)
	c.mu.Unlock()
	return keyboardPrerequisites(checks, input.SelectedKeyboardController())
}

// ObserveKeyboardEnumeration promotes only an explicitly selected, currently
// observed controller from actual READY evidence. Discovery alone never calls it.
// Unselected controller checks remain available, not verified.
func ObserveKeyboardEnumeration(checks []Check, selected, readyController string) []Check {
	result := slices.Clone(checks)
	if selected == "" || selected != readyController ||
		!slices.ContainsFunc(checks, func(check Check) bool { return check.ID == selected && check.State != RequiresAttention }) {
		return result
	}
	for i, check := range result {
		if check.ID == "usb-controllers" || check.ID == selected {
			result[i].State = CheckVerified
			result[i].Message = "Keyboard enumeration was observed on " + selected + ". Each operation re-enumerates; console text acceptance is not verified."
			result[i].NextAction = ""
		}
	}
	return result
}
