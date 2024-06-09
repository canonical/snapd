package snap

import "fmt"

// Runnable represents a runnable element of a snap. This could either be an
// app, a hook, or a component hook.
type Runnable struct {
	// CommandName is the name of the command that is run when this runnable
	// runs.
	CommandName string
	// SecurityTag is the security tag associated with the runnable. Security
	// tags are used by various security subsystems as "profile names" and
	// sometimes also as a part of the file name.
	SecurityTag string
}

// AppRunnable returns a Runnable for the given app.
func AppRunnable(app *AppInfo) Runnable {
	return Runnable{
		CommandName: app.Name,
		SecurityTag: app.SecurityTag(),
	}
}

// HookRunnable returns a Runnable for the given hook. If the hook param points
// to a component, then this runnable will represent a component hook.
func HookRunnable(hook *HookInfo) Runnable {
	if hook.Component == nil {
		return Runnable{
			CommandName: fmt.Sprintf("hook.%s", hook.Name),
			SecurityTag: hook.SecurityTag(),
		}
	}

	return Runnable{
		CommandName: fmt.Sprintf("%s+%s.hook.%s", hook.Snap.SnapName(), hook.Component.Name, hook.Name),
		SecurityTag: hook.SecurityTag(),
	}
}
