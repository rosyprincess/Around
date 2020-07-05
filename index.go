package main

import (
	"context"
	"fmt"

	"github.com/olivere/elastic"
)

const (
	POST_INDEX = "post"
	//internal IP-> does not change
	ES_URL     = "http://10.128.0.2:9200"
	USER_INDEX = "user"
)

func main() {
	//go and elasticSesrch may not be on the same machine after deployment
	client, err := elastic.NewClient(elastic.SetURL(ES_URL))
	if err != nil {
		//handle error
		panic(err)
	}
	defer client.Stop() // optional
	// index is equivalent to database
	// indexExists -> 判断; DO -> 执行
	// context -> request的额外参数:early termination?
	// background -> no additional information
	exists, err := client.IndexExists(POST_INDEX).Do(context.Background())

	if err != nil {
		panic(err)
	}
	if !exists { // index does not exist, default index true
		// "geopoint" -> 2d tree
		// mapping -> schema
		// Index-- Mapping
		mapping := `{
                        "mappings": {
                                "properties": {
                                        "user":     { "type": "keyword", "index": false },
                                        "message":  { "type": "keyword", "index": false },
                                        "location": { "type": "geo_point" },
                                        "url":      { "type": "keyword", "index": false },
                                        "type":     { "type": "keyword", "index": false },
                                        "face":     { "type": "float" }
                                }
                        }
                }`
		_, err := client.CreateIndex(POST_INDEX).Body(mapping).Do(context.Background())
		if err != nil {
			panic(err)
		}
	}
	exists, err = client.IndexExists(USER_INDEX).Do(context.Background())
	if err != nil {
		panic(err)
	}

	if !exists {
		mapping := `{
                        "mappings": {
                                "properties": {
                                        "username": {"type": "keyword"},
                                        "password": {"type": "keyword", "index": false},
                                        "age":      {"type": "long", "index": false},
                                        "gender":   {"type": "keyword", "index": false}
                                }
                        }
                }`
		_, err = client.CreateIndex(USER_INDEX).Body(mapping).Do(context.Background())
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("Post index is created.")
}
