package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/olivere/elastic"
)

const (
	USER_INDEX = "user"
)

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Age      int64  `json:"age"`
	Gender   string `json:"gender"`
}

var mySigningKey = []byte("secret") // secret key is "secret";

func checkUser(username, password string) (bool, error) {
	query := elastic.NewTermQuery("username", username)
	searchResult, err := readFromES(query, USER_INDEX)
	if err != nil {
		return false, err
	}
	// verify user password in DB is the same with input one
	var utype User
	for _, item := range searchResult.Each(reflect.TypeOf(utype)) {
		u := item.(User)
		if u.Password == password {
			fmt.Printf("Login as %s\n", username)
			return true, nil
		}
	}
	return false, nil
}

// pointer *User -> reference减少复制开销
func addUser(user *User) (bool, error) {
	query := elastic.NewTermQuery("username", user.Username)
	searchResult, err := readFromES(query, USER_INDEX) //search in database see if the user name exists
	if err != nil {
		return false, err
	}

	if searchResult.TotalHits() > 0 { // found duplicate
		return false, nil
	}

	err = saveToES(user, USER_INDEX, user.Username) // save user data to ES
	if err != nil {
		return false, err
	}
	fmt.Printf("User is added: %s\n", user.Username)
	return true, nil
}

// http.ResponseWriter is a interface -> does not have pointer
func handlerLogin(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one login request")
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == "OPTIONS" {
		return
	}

	//  Get User information from client
	// decoder -> convert request body to json
	decoder := json.NewDecoder(r.Body)
	var user User
	if err := decoder.Decode(&user); err != nil {
		http.Error(w, "Cannot decode user data from client", http.StatusBadRequest)
		fmt.Printf("Cannot decode user data from client %v\n", err)
		return
	}

	exists, err := checkUser(user.Username, user.Password) // verify log in  info
	if err != nil {
		http.Error(w, "Failed to read user from Elasticsearch", http.StatusInternalServerError)
		fmt.Printf("Failed to read user from Elasticsearch %v\n", err) // fail to verify user info due to ERROR(500)
		return
	}

	if !exists { // does not have error but user info is incorrect or user does not exist(401)
		http.Error(w, "User doesn't exists or wrong password", http.StatusUnauthorized)
		fmt.Printf("User doesn't exists or wrong password\n")
		return
	}
	// log in success -> create token;
	// 加密方法SigningMethodHS25, jwt.SigningMethodHS256 -> header
	// jwt.MapClaims -> payload
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		//username and expiration time
		"username": user.Username,
		"exp":      time.Now().Add(time.Hour * 24).Unix(),
	})
	// 加密, get a string
	tokenString, err := token.SignedString(mySigningKey)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		fmt.Printf("Failed to generate token %v\n", err)
		return
	}
	// return to frontend as request body
	w.Write([]byte(tokenString))
}

// register a new user
func handlerSignup(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one signup request")
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == "OPTIONS" {
		return
	}

	decoder := json.NewDecoder(r.Body)
	var user User
	if err := decoder.Decode(&user); err != nil {
		http.Error(w, "Cannot decode user data from client", http.StatusBadRequest)
		fmt.Printf("Cannot decode user data from client %v\n", err)
		return
	}

	if user.Username == "" || user.Password == "" || regexp.MustCompile(`^[a-z0-9]$`).MatchString(user.Username) {
		http.Error(w, "Invalid username or password", http.StatusBadRequest)
		fmt.Printf("Invalid username or password\n")
		return
	}

	success, err := addUser(&user)
	if err != nil {
		http.Error(w, "Failed to save user to Elasticsearch", http.StatusInternalServerError)
		fmt.Printf("Failed to save user to Elasticsearch %v\n", err)
		return
	}

	if !success {
		http.Error(w, "User already exists", http.StatusBadRequest)
		fmt.Println("User already exists")
		return
	}
	fmt.Printf("User added successfully: %s.\n", user.Username)
}
