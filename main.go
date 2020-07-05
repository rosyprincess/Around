package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath" // get file type
	"reflect"
	"strconv"

	"cloud.google.com/go/storage"
	jwtmiddleware "github.com/auth0/go-jwt-middleware"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/olivere/elastic"
	"github.com/pborman/uuid"
)

const (
	POST_INDEX  = "post"
	DISTANCE    = "200km"
	ES_URL      = "http://10.128.0.2:9200"
	BUCKET_NAME = "chuyun-around"
)

var (
	//hash map -> mapping extension and file type
	mediaTypes = map[string]string{
		".jpeg": "image",
		".jpg":  "image",
		".gif":  "image",
		".png":  "image",
		".mov":  "video",
		".mp4":  "video",
		".avi":  "video",
		".flv":  "video",
		".wmv":  "video",
	}
)

type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Post struct {
	// `json:"user"` is for the json parsing of this User field. Otherwise, by default it's 'User'.
	User     string   `json:"user"`
	Message  string   `json:"message"`
	Location Location `json:"location"` // frondend
	Url      string   `json:"url"`      // GCS
	Type     string   `json:"type"`     // frondend
	Face     float32  `json:"face"`     // from cloud vision api
}

func main() { //start handler
	// MUX add HTTP method restrictions
	fmt.Println("started-service")

	jwtMiddleware := jwtmiddleware.New(jwtmiddleware.Options{
		ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
			return []byte(mySigningKey), nil
		},
		SigningMethod: jwt.SigningMethodHS256,
	})

	r := mux.NewRouter()
	// mapping url,method to handler
	//OPTIONS:用于浏览器验证
	// only souport POST/OPTIONS
	r.Handle("/post", jwtMiddleware.Handler(http.HandlerFunc(handlerPost))).Methods("POST", "OPTIONS")
	r.Handle("/search", jwtMiddleware.Handler(http.HandlerFunc(handlerSearch))).Methods("GET", "OPTIONS")
	r.Handle("/cluster", jwtMiddleware.Handler(http.HandlerFunc(handlerCluster))).Methods("GET", "OPTIONS")
	// sign up and log in does not need token
	r.Handle("/signup", http.HandlerFunc(handlerSignup)).Methods("POST", "OPTIONS")
	r.Handle("/login", http.HandlerFunc(handlerLogin)).Methods("POST", "OPTIONS")

	log.Fatal(http.ListenAndServe(":8080", r))
}

func handlerPost(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one post request")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")                           // 跨域访问：open to all frontend
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization") // 验证login: access control

	if r.Method == "OPTIONS" { // OPTIONS request -> 直接返回
		return
	}
	user := r.Context().Value("user")
	claims := user.(*jwt.Token).Claims
	username := claims.(jwt.MapClaims)["username"]

	// Read parameter from client
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lon, _ := strconv.ParseFloat(r.FormValue("lon"), 64)

	p := &Post{
		User:    username.(string),
		Message: r.FormValue("message"),
		Location: Location{
			Lat: lat,
			Lon: lon,
		},
	}
	// save image to GCS; "image" -> in postmen
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Image is not available", http.StatusBadRequest)
		fmt.Printf("Image is not available %v\n", err)
		return
	}

	suffix := filepath.Ext(header.Filename)
	if t, ok := mediaTypes[suffix]; ok {
		p.Type = t
	} else {
		p.Type = "unknown"
	}

	id := uuid.New()
	mediaLink, err := saveToGCS(file, id)
	if err != nil {
		http.Error(w, "Failed to save image to GCS", http.StatusInternalServerError)
		fmt.Printf("Failed to save image to GCS %v\n", err)
		return
	}
	p.Url = mediaLink
	// annotate image with vision api
	if p.Type == "image" {
		uri := fmt.Sprintf("gs://%s/%s", BUCKET_NAME, id) // format uri; Sprintf(save to var rui insteade of just printing in termnal)
		if score, err := annotate(uri); err != nil {      //调用annotate in vision.go (same package, not import)
			http.Error(w, "Failed to annotate image", http.StatusInternalServerError)
			fmt.Printf("Failed to annotate the image %v\n", err)
			return
		} else { // no error; get score
			p.Face = score
		}
	}
	// save post to ES
	err = saveToES(p, POST_INDEX, id)
	if err != nil {
		http.Error(w, "Failed to save post to Elasticsearch", http.StatusInternalServerError)
		fmt.Printf("Failed to save post to Elasticsearch %v\n", err)
		return
	}

}

// handle request sent to cluster; similar to handle search
func handlerCluster(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one cluster request")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")

	if r.Method == "OPTIONS" {
		return
	}

	term := r.URL.Query().Get("term")
	query := elastic.NewRangeQuery(term).Gte(0.9)

	searchResult, err := readFromES(query, POST_INDEX)
	if err != nil {
		http.Error(w, "Failed to read from Elasticsearch", http.StatusInternalServerError)
		return
	}

	posts := getPostFromSearchResult(searchResult)
	js, err := json.Marshal(posts)
	if err != nil {
		http.Error(w, "Failed to parse post object", http.StatusInternalServerError)
		fmt.Printf("Failed to parse post object %v\n", err)
		return
	}
	w.Write(js)
}

func handlerSearch(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one request for search")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")

	if r.Method == "OPTIONS" {
		return
	}

	// Get method for search
	// r.URL -> request web address
	// .Query() -> after ?
	// string to float: strconv.ParseFloat(string, decimal)
	// - -> err
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, _ := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	// range is optional
	ran := DISTANCE
	// val is only valid inside if statement
	if val := r.URL.Query().Get("range"); val != "" {
		ran = val + "km" // must add unit
	}
	fmt.Println("range is ", ran)
	// query use location
	query := elastic.NewGeoDistanceQuery("location")
	query = query.Distance(ran).Lat(lat).Lon(lon) //only search for location within range
	searchResult, err := readFromES(query, POST_INDEX)
	// return http error instead of killing the program
	if err != nil {
		http.Error(w, "Failed to read post from Elasticsearch", http.StatusInternalServerError)
		fmt.Printf("Failed to read post from Elasticsearch %v.\n", err)
		return
	}
	//change to post array
	posts := getPostFromSearchResult(searchResult)
	// change to json array and return to frond end
	js, err := json.Marshal(posts)
	if err != nil {
		http.Error(w, "Failed to parse posts into JSON format", http.StatusInternalServerError)
		fmt.Printf("Failed to parse posts into JSON format %v.\n", err)
		return
	}
	w.Write(js)
}

// get search result
func readFromES(query elastic.Query, index string) (*elastic.SearchResult, error) {
	client, err := elastic.NewClient(elastic.SetURL(ES_URL))
	if err != nil {
		return nil, err
	}

	searchResult, err := client.Search().
		Index(index).
		Query(query).
		Pretty(true).
		Do(context.Background())
	if err != nil {
		return nil, err
	}

	return searchResult, nil
}

// get restult that satisfies condition
func getPostFromSearchResult(searchResult *elastic.SearchResult) []Post {
	var ptype Post
	var posts []Post

	for _, item := range searchResult.Each(reflect.TypeOf(ptype)) {
		p := item.(Post)
		posts = append(posts, p)
	}

	return posts
}

// io.Reader -> file reader; filename
func saveToGCS(r io.Reader, objectName string) (string, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", err
	}
	bucket := client.Bucket(BUCKET_NAME)         //bucket instance
	if _, err := bucket.Attrs(ctx); err != nil { //check if bucket exists
		return "", err
	}
	object := bucket.Object(objectName)
	wc := object.NewWriter(ctx)
	if _, err := io.Copy(wc, r); err != nil {
		return "", err
	}

	if err := wc.Close(); err != nil {
		return "", err
	}
	// bucket access: default is user only(run on virtual machine -> user is service account)
	// open read access to all users
	if err := object.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return "", err
	}

	attrs, err := object.Attrs(ctx)
	if err != nil {
		return "", err
	}

	fmt.Printf("Image is saved to GCS: %s\n", attrs.MediaLink)
	return attrs.MediaLink, nil
}

// save info to database ES
func saveToES(i interface{}, index string, id string) error {
	client, err := elastic.NewClient(elastic.SetURL(ES_URL))
	if err != nil {
		return err
	}

	_, err = client.Index(). // in project 1: => prepared statement
					Index(index). // statement.set
					Id(id).       // (primay key)
					BodyJson(i).
					Do(context.Background()) // excecute

	if err != nil {
		return err
	}

	return nil
}
