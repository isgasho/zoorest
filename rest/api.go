package rest

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"github.com/samuel/go-zookeeper/zk"
)

// Ls ...
type Ls struct {
	Childrens []string `json:"childrens"`
	Path      string   `json:"path"`
	State     string   `json:"state"`
	Error     string   `json:"error"`
	ZkStat    *zk.Stat `json:"zkstat"`
}

// Get ...
type Get struct {
	Path   string   `json:"path"`
	State  string   `json:"state"`
	Error  string   `json:"error"`
	ZkStat *zk.Stat `json:"zkstat"`
	Data   []byte   `json:"data"`
}

type GetJSON struct {
	Path   string      `json:"path"`
	State  string      `json:"state"`
	Error  string      `json:"error"`
	ZkStat *zk.Stat    `json:"zkstat"`
	Data   interface{} `json:"data"`
}

type CorsOptions struct {
	Enabled        bool
	DebugEnabled   bool
	AllowedOrigins []string
}

// LS ...
func (zk ZooNode) LS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path := vars["path"]

	ch := make(chan Ls)

	go func() { ch <- zk.GetChildrens(path) }()

	childrens := <-ch
	data, err := json.Marshal(childrens)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("JSON parsing failure"))
		return
	}
	if childrens.Error != "" {
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	w.WriteHeader(200)
	w.Write(data)
}

// ReadRequestBody ...
func ReadRequestBody(req *http.Request) ([]byte, error) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return []byte(""), err
	}

	return body, err
}

// UP ...
func (zk ZooNode) UP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path := vars["path"]

	ch := make(chan string)
	// Read request body as []byte
	content, err := ReadRequestBody(r)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}

	// Create node
	if r.Method == "PUT" {
		go func() { ch <- zk.CreateNode(path, content) }()
	} else if r.Method == "POST" {
		go func() { ch <- zk.UpdateNode(path, content) }()
	} else if r.Method == "PATCH" {
		go func() { ch <- zk.CreateChild(path, content) }()
	} else {
		e := strings.Join([]string{"Method", r.Method, "not alowed"}, " ")
		w.WriteHeader(500)
		w.Write([]byte(e))
		return
	}
	defer r.Body.Close()

	state := <-ch

	w.WriteHeader(200)
	w.Write([]byte(state))
}

// RM ...
func (zk ZooNode) RM(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path := vars["path"]

	if path == "/" {
		e := "Skiping root path"
		w.WriteHeader(500)
		w.Write([]byte(e))
		return
	}

	var rmPath string
	rmPath = strings.Join([]string{zk.Path, path}, "")

	if strings.Contains(rmPath, "//") {
		rmPath = strings.Replace(rmPath, "//", "/", 1)
	}

	go func() { zk.RMR(rmPath) }()

	w.WriteHeader(200)
	w.Write([]byte(path))
}

// GET ...
func (zk ZooNode) GET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path := vars["path"]

	ch := make(chan Get)

	go func() { ch <- zk.GetNode(path) }()

	childrens := <-ch
	data, err := json.Marshal(childrens)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("JSON parsing failure"))
		return
	}

	if childrens.Error != "" {
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	w.WriteHeader(200)
	w.Write(data)
}

// GetJSON marshals the node data to the JSON
func (zk ZooNode) GetJSON(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	path := vars["path"]

	ch := make(chan Get)

	go func() { ch <- zk.GetNode(path) }()

	childrens := <-ch
	data, err := json.Marshal(childrens)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("JSON parsing failure"))
		return
	}

	if childrens.Error != "" {
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	dataJson, err := unmarhalNodeData(childrens.Data)
	if err != nil {
		w.WriteHeader(500)
		childrens.Error = "JSON parsing failure: " + err.Error()
		w.Write(errorUnmarshal(childrens))
		return
	}

	getJson := GetJSON{
		Path:   childrens.Path,
		Error:  childrens.Error,
		State:  childrens.State,
		ZkStat: childrens.ZkStat,
		Data:   dataJson,
	}
	if data, err := json.Marshal(getJson); err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(200)
		w.Write(data)
	}
}

// Serve ...
func Serve(listen string, zk ZooNode, cors_options CorsOptions) {
	r := mux.NewRouter()

	// API v1
	r.HandleFunc("/v1/ls{path:[[:print:]]+}", zk.LS).Methods("GET", "LIST")
	r.HandleFunc("/v1/get{path:[[:print:]]+}", zk.GET).Methods("GET")
	r.HandleFunc("/v1/get{path:[[:print:]]+}+json", zk.GetJSON).Methods("GET")
	r.HandleFunc("/v1/rmr{path:[[:print:]]+}", zk.RM).Methods("DELETE")
	r.HandleFunc("/v1/up{path:[[:print:]]+}", zk.UP).Methods("PUT", "POST", "PATCH")

	// API v2
	r.HandleFunc("/v2{path:[[:print:]]+}", zk.LS).Methods("LIST")
	r.HandleFunc("/v2{path:[[:print:]]+}", zk.GET).Methods("GET")
	r.HandleFunc("/v2{path:[[:print:]]+}+json", zk.GetJSON).Methods("GET")
	r.HandleFunc("/v2{path:[[:print:]]+}", zk.RM).Methods("DELETE")
	r.HandleFunc("/v2{path:[[:print:]]+}", zk.UP).Methods("PUT", "POST", "PATCH")

	var handler http.Handler

	if cors_options.Enabled {
		c := cors.New(cors.Options{
			AllowedMethods: []string{"GET", "LIST", "DELETE", "PUT", "POST", "PATCH"},
			AllowedOrigins: cors_options.AllowedOrigins,
			// Enable Debugging for testing, consider disabling in production
			Debug: cors_options.DebugEnabled,
		})

		// decorate with cors handler
		handler = c.Handler(r)
	} else {
		// default to no
		handler = r
	}

	srv := http.Server{
		Handler:      handler,
		Addr:         listen,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Print("Listening API on ", listen)
	log.Fatal(srv.ListenAndServe())
}

func unmarhalNodeData(data []byte) (interface{}, error) {
	var content interface{}
	err := json.Unmarshal(data, &content)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func errorUnmarshal(get Get) []byte {
	get.Data = nil
	get.State = "ERROR"
	data, err := json.Marshal(get)
	if err != nil {
		return nil
	}
	return data
}
