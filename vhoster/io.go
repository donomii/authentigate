package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)


func SaveConfig(config *Config) {
	payload, err := json.MarshalIndent(config, "", "	")
	if err != nil {
		panic("Could not marshall config")
	}
	ioutil.WriteFile(fmt.Sprintf("config.json"), payload, 0600)
}

//Load user data
func LoadConfig(file string) *Config {
	var config Config
	raw, err := ioutil.ReadFile(file)
	if err != nil {
		log.Printf("Unable to read from file %a: %v", file, err)
		panic(fmt.Sprintf("Unable to read from file %a: %v", file, err))
	}

	json.Unmarshal(raw, &config)
	return &config
}
