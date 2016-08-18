package tools

type setCommand struct {
	baseCommand
}

func init() {
	addCommand("set", &setCommand{})
}

func (s *setCommand) Execute(args []string) error {
	// TODO: Talk to the handler to take care of the set request.
	return nil
}
