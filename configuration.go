package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Configuration struct {
	Debug    bool `yaml:"debug"`
	Database struct {
		Host             string `yaml:"host"`
		User             string `yaml:"user"`
		Password         string `yaml:"pass"`
		DB               string `yaml:"db"`
		ConnectString    string
		AstConnectString string
	} `yaml:"database"`
	InfluxDB struct {
		Host   string `yaml:"host"`
		User   string `yaml:"user"`
		Pass   string `yaml:"pass"`
		DB     string `yaml:"db"`
		PBXTag string `yaml:"pbx_tag"`
	} `yaml:"influx_db"`
	AMI struct {
		Host          string `yaml:"host"`
		Port          int    `yaml:"port"` // standard is 5038 for Asterisk AMI
		User          string `yaml:"user"`
		Pass          string `yaml:"pass"`
		ConnectString string // pbxz.simplybits.net:5038
	} `yaml:"ami"`
	Redis struct {
		Host string `yaml:"host"`
	} `yaml:"redis"`
	Web struct {
		TemplatePath      string `yaml:"templatepath"`
		SSLBindAddr       string `yaml:"sslbindaddr"`
		SSLCertFile       string `yaml:"sslcertfile"`
		SSLPrivateKeyFile string `yaml:"sslprivatekeyfile"`
		AcmePort          int    `yaml:"acmeport"`
	} `yaml:"web"`
}

func (c *Configuration) Load(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	if err = decoder.Decode(c); err != nil {
		return fmt.Errorf("failed to decode config file: %w", err)
	}

	c.Database.ConnectString = c.Database.User + ":" + c.Database.Password +
		"@tcp(" + c.Database.Host + ":3306)/" + c.Database.DB +
		"?parseTime=true&loc=America%2FPhoenix"
	c.Database.AstConnectString = c.Database.User + ":" + c.Database.Password +
		"@tcp(" + c.Database.Host + ":3306)/AstConfig" +
		"?parseTime=true&loc=America%2FPhoenix"

	c.AMI.ConnectString = fmt.Sprintf("%s:%d", c.AMI.Host, c.AMI.Port)

	if c.AMI.Port == 0 {
		c.AMI.Port = 5038
	}
	if len(c.AMI.Host) == 0 {
		c.AMI.Host = "127.0.0.1"
	}
	c.AMI.ConnectString = fmt.Sprintf("%s:%d", c.AMI.Host, c.AMI.Port)

	if c.Web.AcmePort == 0 {
		c.Web.AcmePort = 80
	}

	if len(c.Redis.Host) > 5 {
		if strings.IndexRune(c.Redis.Host, ':') == -1 {
			c.Redis.Host = c.Redis.Host + ":6379"
		}
	}

	return nil
}

func (c *Configuration) HasInfluxConfig() bool {
	if len(c.InfluxDB.Host) > 0 && len(c.InfluxDB.DB) > 0 && len(c.InfluxDB.User) > 0 && len(c.InfluxDB.Pass) > 0 && len(c.InfluxDB.PBXTag) > 0 {
		return true
	}
	return false
}

func (c *Configuration) Print() {
	logger.Debugf("Database Server: %s\n", c.Database.Host)
	logger.Debugf("Database User: %s\n", c.Database.User)
	logger.Debugf("Database Name: %s\n", c.Database.DB)
	logger.Debugf("InfluxDB Host: %s\n", c.InfluxDB.Host)
	logger.Debugf("InfluxDB User: %s\n", c.InfluxDB.User)
	logger.Debugf("InfluxDB DB: %s\n", c.InfluxDB.DB)
	logger.Debugf("AMI Host: %s\n", c.AMI.Host)
	logger.Debugf("AMI Port: %d\n", c.AMI.Port)
	logger.Debugf("AMI User: %s\n", c.AMI.User)
	logger.Debugf("Redis Host: %s\n", c.Redis.Host)
	logger.Debugf("Web Template Path: %s\n", c.Web.TemplatePath)
	logger.Debugf("Web SSL Bind Address: %s\n", c.Web.SSLBindAddr)
	logger.Debugf("Web SSL Cert File: %s\n", c.Web.SSLCertFile)
	logger.Debugf("Web SSL Private Key File: %s\n", c.Web.SSLPrivateKeyFile)
}
