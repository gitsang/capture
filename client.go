package main

import (
	"errors"
	"time"

	"github.com/RPbro/javdbapi"
)

type Client struct {
	*javdbapi.Client
}

type ClientOptionFunc func(*Client)

func NewClient(optfs ...ClientOptionFunc) *Client {
	c := &Client{
		Client: javdbapi.NewClient(
			javdbapi.WithDomain("https://javdb.com"),
			javdbapi.WithUserAgent("Mozilla/5.0 (Macintosh; ..."),
			javdbapi.WithTimeout(time.Second*30),
		),
	}
	for _, apply := range optfs {
		apply(c)
	}
	return c
}

func (c *Client) Get(path string) (*javdbapi.JavDB, error) {
	result, err := c.GetFirst().
		SetRaw("https://javdb.com" + path).First()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) SearchByCode(code string) (*javdbapi.JavDB, error) {
	results, err := c.GetSearch().SetQuery(code).Get()
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
