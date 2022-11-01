package main

import (
	"fmt"
	"io"
	"net/http"
)

type GithubClient struct {
	token string
}

func (client *GithubClient) doRequest(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", client.token))
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode > 200 {
		return nil, fmt.Errorf("received status %d", resp.StatusCode)
	}
	defer func(body io.Closer) {
		err := body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
