package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/olekukonko/tablewriter"
)

func cmdSearch(args []string) error {
	search_term := args[0]

	const SEARCH_URI = "https://search.apps.ubuntu.com/api/v1/search?q=%s"

	url := fmt.Sprintf(SEARCH_URI, search_term)
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	// set headers
	req.Header.Set("Accept", "application/hal+json")
	//FIXME: hardcoded
	req.Header.Set("X-Ubuntu-Frameworks", "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Architecture", "amd64")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	searchData := make(map[string]interface{})
	body, err := ioutil.ReadAll(resp.Body)
	//log.Print(string(body))
	if err != nil {
		return err
	}
	err = json.Unmarshal(body, &searchData)
	if err != nil {
		return nil
	}
	embedded := searchData["_embedded"].(map[string]interface{})
	packages := embedded["clickindex:package"].([]interface{})

	// FIXME: how to wrap tablewriter.NewWriter() so that we always
	//        get the no row/col/center sepators?
	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowSeparator("")
	table.SetColumnSeparator("")
	table.SetCenterSeparator("")

	for _, raw := range packages {
		pkg := raw.(map[string]interface{})
		//fmt.Printf("%s (%s) - %s \n", pkg["name"], pkg["version"], pkg["title"])
		table.Append([]string{pkg["name"].(string), pkg["version"].(string), pkg["title"].(string)})
	}
	table.Render()

	return nil
}
