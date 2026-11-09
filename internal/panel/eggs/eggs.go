// Package eggs reads Pterodactyl's eggs: the JSON files that say how to install and run a game server
// (its images, startup command, installer, settings and the config files to fill in). Eggs come from
// people's own files and community repositories, so everything in one is checked before it's used.
package eggs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Egg is the part of an egg Bastion uses.
type Egg struct {
	Name        string `json:"name"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Images      Images `json:"docker_images"`
	Image       string `json:"image"` // older eggs have one image
	Startup     string `json:"startup"`
	Config      struct {
		Files   string `json:"files"` // JSON in a string: the config files to fill in
		Startup string `json:"startup"`
		Stop    string `json:"stop"`
	} `json:"config"`
	Scripts struct {
		Installation struct {
			Script     string `json:"script"`
			Container  string `json:"container"`
			Entrypoint string `json:"entrypoint"`
		} `json:"installation"`
	} `json:"scripts"`
	Variables []Variable `json:"variables"`
}

// Variable is a setting each game server chooses, passed to it as an environment variable.
type Variable struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Env          string `json:"env_variable"`
	Default      string `json:"default_value"`
	UserViewable bool   `json:"user_viewable"`
	UserEditable bool   `json:"user_editable"`
	Rules        string `json:"rules"`
}

var (
	imageRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]{0,254}$`)
	envName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	shell    = regexp.MustCompile(`^[a-z/]{1,40}$`)
)

// Parse reads and checks an egg.
func Parse(raw []byte) (*Egg, error) {
	var e Egg
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, errors.New("that isn't an egg: it isn't JSON Pterodactyl exports")
	}
	var defaults struct {
		Variables []struct {
			Default json.RawMessage `json:"default_value"`
		} `json:"variables"`
	}
	_ = json.Unmarshal(raw, &defaults)
	e.Name = strings.TrimSpace(e.Name)
	if e.Image != "" && len(e.Images) == 0 {
		e.Images = Images{{e.Image, e.Image}}
	}
	switch {
	case e.Name == "" || len(e.Name) > 100:
		return nil, errors.New("the egg has no name")
	case e.Startup == "" || len(e.Startup) > 4<<10:
		return nil, errors.New("the egg has no startup command")
	case len(e.Images) == 0:
		return nil, errors.New("the egg has no image to run")
	}
	for _, image := range e.Images {
		if !imageRef.MatchString(image.Ref) {
			return nil, fmt.Errorf("the egg's image %q (%s) isn't an image name", image.Ref, image.Label)
		}
	}
	install := e.Scripts.Installation
	if install.Script != "" {
		if !imageRef.MatchString(install.Container) || !shell.MatchString(install.Entrypoint) {
			return nil, errors.New("the egg's installer has an odd image or shell")
		}
	}
	seen := map[string]bool{}
	for i := range e.Variables {
		v := &e.Variables[i]
		if !envName.MatchString(v.Env) || seen[v.Env] {
			return nil, fmt.Errorf("the egg's variable %q can't be used", v.Env)
		}
		seen[v.Env] = true
		// Some eggs write a number as a default rather than text.
		if v.Default == "" && i < len(defaults.Variables) && len(defaults.Variables[i].Default) > 0 && defaults.Variables[i].Default[0] != '"' {
			v.Default = strings.Trim(string(defaults.Variables[i].Default), " ")
			if v.Default == "null" {
				v.Default = ""
			}
		}
	}
	if _, err := e.ConfigFiles(); err != nil {
		return nil, err
	}
	return &e, nil
}

// Image is one of the images an egg can run with, like "Java 21": ghcr.io/pterodactyl/yolks:java_21.
type Image struct {
	Label string `json:"label"`
	Ref   string `json:"image"`
}

// Images keeps an egg's images in the order it lists them: the first is the default.
type Images []Image

func (list *Images) UnmarshalJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errors.New("docker_images isn't a list of images")
	}
	for dec.More() {
		label, err := dec.Token()
		if err != nil {
			return err
		}
		var ref string
		if err := dec.Decode(&ref); err != nil {
			return err
		}
		*list = append(*list, Image{Label: label.(string), Ref: ref})
	}
	return nil
}

// HasImage says whether an image is one of the egg's.
func (e *Egg) HasImage(image string) bool {
	for _, i := range e.Images {
		if i.Ref == image {
			return true
		}
	}
	return false
}

// ConfigFile is a config file the egg fills in before the game starts: in it, each key is set to its value.
type ConfigFile struct {
	File   string            `json:"file"`
	Parser string            `json:"parser"` // properties, ini, json, yaml, xml or file
	Find   map[string]string `json:"find"`
}

var safeFile = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/ -]{0,200}$`)

// ConfigFiles reads the egg's config file edits.
func (e *Egg) ConfigFiles() ([]ConfigFile, error) {
	if strings.TrimSpace(e.Config.Files) == "" || e.Config.Files == "{}" || e.Config.Files == "[]" {
		return nil, nil
	}
	var files map[string]struct {
		Parser string                     `json:"parser"`
		Find   map[string]json.RawMessage `json:"find"`
	}
	if err := json.Unmarshal([]byte(e.Config.Files), &files); err != nil {
		return nil, errors.New("the egg's config files can't be read")
	}
	var list []ConfigFile
	for name, f := range files {
		if !safeFile.MatchString(name) || strings.Contains(name, "..") {
			return nil, fmt.Errorf("the egg names an odd config file: %q", name)
		}
		cf := ConfigFile{File: name, Parser: f.Parser, Find: map[string]string{}}
		for key, raw := range f.Find {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				s = string(raw) // a number, true or an object: set as written
			}
			cf.Find[key] = s
		}
		list = append(list, cf)
	}
	return list, nil
}

// Placeholders is what an egg's {{…}} placeholders are filled in with: the game's own values.
type Placeholders struct {
	Port     int
	MemoryMB int
	Env      map[string]string
}

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)

// Fill replaces placeholders: {{SERVER_PORT}} and the egg's own variables, and the config files' longer
// forms like {{server.build.default.port}} and {{server.build.env.SERVER_JARFILE}}.
func (p Placeholders) Fill(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		key := placeholder.FindStringSubmatch(m)[1]
		switch key {
		case "SERVER_PORT", "server.build.default.port":
			return strconv.Itoa(p.Port)
		case "SERVER_MEMORY", "server.build.memory":
			return strconv.Itoa(p.MemoryMB)
		case "SERVER_IP", "server.build.default.ip":
			return "0.0.0.0"
		}
		for _, prefix := range []string{"server.build.env.", "env."} {
			key = strings.TrimPrefix(key, prefix)
		}
		if v, ok := p.Env[key]; ok {
			return v
		}
		return m
	})
}

// Check checks a variable's value against its rules, which are Laravel's: "required|string|max:20".
// The ones eggs use are understood; any others are ignored.
func (v Variable) Check(value string) error {
	rules := strings.Split(v.Rules, "|")
	nullable := false
	for _, r := range rules {
		if r == "nullable" {
			nullable = true
		}
	}
	if value == "" {
		for _, r := range rules {
			if r == "required" {
				return fmt.Errorf("%s is needed", v.Name)
			}
		}
		if nullable {
			return nil
		}
	}
	numeric := false
	for _, r := range rules {
		name, arg, _ := strings.Cut(r, ":")
		switch name {
		case "numeric", "integer":
			numeric = true
			if _, err := strconv.ParseFloat(value, 64); err != nil || (name == "integer" && strings.Contains(value, ".")) {
				return fmt.Errorf("%s should be a number", v.Name)
			}
		case "boolean":
			if value != "0" && value != "1" && value != "true" && value != "false" {
				return fmt.Errorf("%s should be true or false (1 or 0)", v.Name)
			}
		case "in":
			if !contains(strings.Split(arg, ","), value) {
				return fmt.Errorf("%s should be one of %s", v.Name, strings.ReplaceAll(arg, ",", ", "))
			}
		case "max", "min", "between":
			lo, hi := -1.0, -1.0
			switch name {
			case "max":
				hi, _ = strconv.ParseFloat(arg, 64)
			case "min":
				lo, _ = strconv.ParseFloat(arg, 64)
			default:
				a, b, _ := strings.Cut(arg, ",")
				lo, _ = strconv.ParseFloat(a, 64)
				hi, _ = strconv.ParseFloat(b, 64)
			}
			size := float64(len(value))
			if numeric {
				size, _ = strconv.ParseFloat(value, 64)
			}
			if (lo >= 0 && size < lo) || (hi >= 0 && size > hi) {
				return fmt.Errorf("%s is out of range (%s)", v.Name, r)
			}
		case "regex":
			// PHP writes these as /pattern/; Go's regexp understands most of them.
			pattern := strings.TrimSuffix(strings.TrimPrefix(arg, "/"), "/")
			if re, err := regexp.Compile(pattern); err == nil && !re.MatchString(value) {
				return fmt.Errorf("%s isn't in the right form", v.Name)
			}
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
