package partition

type UbootBootLoader struct {
    partition *Partition
}

func (u *UbootBootLoader) Name() string {
    return "u-boot"
}

func (u *UbootBootLoader) Installed() bool {
    // crude heuristic
    err := FileExists("/boot/uEnv.txt")

    if err != nil {
        return true
    }

    return false
}

func (u *UbootBootLoader) ToggleRootFS(p *Partition) (err error) {
    // FIXME: copy from python!
    return err
}

func (u *UbootBootLoader) GetAllBootVars() (vars []string, err error) {
    // FIXME
    return vars, err
}

func (u *UbootBootLoader) GetBootVar(name string) (value string) {
    // FIXME
    return value
}

func (u *UbootBootLoader) SetBootVar(name, value string) (err error) {
    // FIXME
    return err
}

func (u *UbootBootLoader) GetNextBootRootLabel() (label string) {
    // FIXME
    return label
}

func (u *UbootBootLoader) GetCurrentBootRootLabel() (label string) {
    // FIXME
    return label
}
