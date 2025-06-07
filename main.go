package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	client := NewClient()
	result, err := client.SearchByCode("MIDA-180")
	if err != nil {
		panic(err)
	}
	resultJsonBytes, _ := json.Marshal(result)
	fmt.Println(string(resultJsonBytes))
}
