package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"text/tabwriter"
)

const SEARCH_URI = "https://search.apps.ubuntu.com/api/v1/search?q=%s"

func cmdSearch(args []string) error {
	search_term := args[0]

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
	//fmt.Print(string(body))
	if err != nil {
		return err
	}
	err = json.Unmarshal(body, &searchData)
	if err != nil {
		return nil
	}
	embedded := searchData["_embedded"].(map[string]interface{})
	packages := embedded["clickindex:package"].([]interface{})

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, raw := range packages {
		pkg := raw.(map[string]interface{})
		fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", pkg["name"], pkg["version"], pkg["title"]))
	}
	w.Flush()

	return nil
}
