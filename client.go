package main

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gitsang/capture/pkg/javdbapi"
)

type Client struct {
	*javdbapi.Client
}

type ClientOptionFunc func(*Client)

func NewClient(optfs ...ClientOptionFunc) *Client {
	proxyURL, err := url.Parse("http://pi.xm1.c8g.top:7890")
	if err != nil {
		panic(err)
	}
	httpCli := &http.Client{
		Timeout: time.Second * 30,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	c := &Client{
		Client: javdbapi.NewClient(
			javdbapi.WithDomain("https://javdb.com"),
			javdbapi.WithUserAgent("Mozilla/5.0 (Macintosh; ..."),
			javdbapi.WithTimeout(time.Second*30),
			javdbapi.WithHttpClient(httpCli),
		),
	}
	for _, apply := range optfs {
		apply(c)
	}
	return c
}

func (c *Client) Get(path string) (*javdbapi.Item, error) {
	result, err := c.Client.GetFirst().
		SetRaw("https://javdb.com" + path).First()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) SearchByCode(code string) (*javdbapi.Item, error) {
	results, err := c.Client.GetSearch().SetQuery(code).Get()
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, errors.New("no results")
	}

	result := results[0]
	if result.Code != code {
		return nil, errors.New("not found")
	}

	return c.Get(result.Path)
}
