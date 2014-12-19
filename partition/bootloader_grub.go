package partition

type GrubBootLoader struct {
    partition *Partition
}

func (g *GrubBootLoader) Name() string {
    return "grub"
}

func (g *GrubBootLoader) Installed() bool {
    // crude heuristic
    err := FileExists("/boot/grub/grub.cfg")

    if err != nil {
        return true
    }

    return false
}
func (g *GrubBootLoader) ToggleRootFS(p *Partition) (err error) {
	var args []string
	var other *BlockDevice

    g.partition = p

	other = p.OtherRootPartition()

	args = append(args, "grub-install")
	args = append(args, other.parentName)

	// install grub
	err = p.RunInChroot(args)
	if err != nil {
		return err
	}

	args = nil
	args = append(args, "update-grub")

	// create the grub config
	err = p.RunInChroot(args)

    return err
}

func (g *GrubBootLoader) GetAllBootVars() (vars []string, err error) {
    // FIXME
    return vars, err
}

func (g *GrubBootLoader) GetBootVar(name string) (value string) {
    // FIXME
    return value
}

func (g *GrubBootLoader) SetBootVar(name, value string) (err error) {
    // FIXME
    return err
}

func (g *GrubBootLoader) GetNextBootRootLabel() (label string) {
    // FIXME
    return label
}

func (g *GrubBootLoader) GetCurrentBootRootLabel() (label string) {
    // FIXME
    return label
}
