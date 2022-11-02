package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GithubClient struct {
	baseUrl, token string
}

func (client *GithubClient) fetchResourceAsJson(resource string, data interface{}) error {
	req, err := http.NewRequest(http.MethodGet, resource, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", client.token))
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer closeOrPanic(resp.Body)

	if resp.StatusCode > 299 {
		return fmt.Errorf("received status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, &data)
}

func closeOrPanic(body io.Closer) {
	if err := body.Close(); err != nil {
		panic(err)
	}
}
