package snappy

import ()

func CmdBooted(args []string) (err error) {

	repo := []Repository{NewSystemImageRepository()}

	parts, err := repo[0].GetInstalled()

	if err != nil {
		return err
	}

	return parts[0].(*SystemImagePart).MarkBootSuccessful()
}
