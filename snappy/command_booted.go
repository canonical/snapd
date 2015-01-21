package snappy

import ()

func CmdBooted(args []string) (err error) {

	parts, err := GetInstalledSnappsByType("core")
	if err != nil {
		return err
	}

	return parts[0].(*SystemImagePart).MarkBootSuccessful()
}
