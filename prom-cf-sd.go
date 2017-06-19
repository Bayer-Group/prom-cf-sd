package main

import (
    "flag"
    "net/http"
    "encoding/json"
    "log"
    "time"
    "os"
    "strconv"
    "io"
    "io/ioutil"
 
    "github.com/cloudfoundry-community/go-cfclient"    
)

var done chan bool
var target chan []TargetGroup

var (
	ApiAddress = flag.String(
		"api.address", "",
		"Cloud Foundry API Address ($API_ADDRESS).",
	)
        
	ClientID = flag.String(
                "api.id", "", 
                "Cloud Foundry UAA ClientID ($CF_CLIENT_ID).",
        )

	ClientSecret = flag.String(
                "api.secret", "", 
                "Cloud Foundry UAA ClientSecret ($CF_CLIENT_SECRET).",
        )

	SkipSSL = flag.Bool(
                "skip.ssl", false, 
                "Disalbe SSL validation ($SKIP_SSL).",
        )

	Frequency = flag.Uint(
                "update.frequency", 3,
                "SD Update frequency in minutes ($FREQUENCY).",
        )

	OutputFile = flag.String(
                "out.file", "/tmp/cf_targets.json",
                "Location to write target json ($OUTPUT_FILE).",
        )
)

// TargetGroup is the target group read by Prometheus.
type TargetGroup struct {
	Targets []string          `json:"targets,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type SpaceInfo struct {
	Name             string `json:"name"`
        OrgName		 string `json:"orgname"`
}

func overrideFlagsWithEnvVars() {
	overrideWithEnvVar("API_ADDRESS", ApiAddress)
	overrideWithEnvVar("CF_CLIENT_ID", ClientID)
	overrideWithEnvVar("CF_CLIENT_SECRET", ClientSecret)
	overrideWithEnvBool("SKIP_SSL", SkipSSL)
	overrideWithEnvUint("FREQUENCY", Frequency)
	overrideWithEnvVar("OUTPUT_FILE", OutputFile)
}

func overrideWithEnvVar(name string, value *string) {
	envValue := os.Getenv(name)
	if envValue != "" {
		*value = envValue
	}
}

func overrideWithEnvUint(name string, value *uint) {
	envValue := os.Getenv(name)
	if envValue != "" {
		intValue, err := strconv.Atoi(envValue)
		if err != nil {
			log.Fatalln("Invalid `%s`: %s", name, err)
		}
		*value = uint(intValue)
	}
}

func overrideWithEnvBool(name string, value *bool) {
	envValue := os.Getenv(name)
	if envValue != "" {
		var err error
		*value, err = strconv.ParseBool(envValue)
		if err != nil {
			log.Fatalf("Invalid `%s`: %s", name, err)
		}
	}
}

func updateTargetList (client *cfclient.Client, apiaddress string) {
   //Create one right away
   go createTargetList(client, apiaddress)  

   //Start the timer, default 3 min to update.  If previous runs still going then don't launch another
   tick := time.Tick(time.Duration(*Frequency) * time.Minute)
   for {
     select {
     case <-tick:
	select {
      		case <- done:
        		go createTargetList(client, apiaddress)	
		default:
			log.Printf("Previous run not finished.  Skipping....")
      	}
     }
   }
}

func createTargetList(client *cfclient.Client, apiaddress string) {
	var applistchunks [][]cfclient.App
	var tgroups []TargetGroup

	// create a couple maps for org/space lookup rather than using inline-depth on the api calls.  This speeds things up significantly

	log.Println("Generating target list")
     	apps,err := client.ListAppsByQuery(nil)
	if err != nil {
		log.Printf("Error generating list of apps from CF: %v", err)
	}  

	orgs,err := client.ListOrgs()
        if err != nil {
                log.Printf("Error generating list of orgs from CF: %v", err)
        }

	// create map of org guid to org name
	orgmap := map[string]string{}
	for _, org := range orgs {
		orgmap[org.Guid] = org.Name
	} 	

        spaces,err := client.ListSpaces()
        if err != nil {
                log.Printf("Error generating list of spaces from CF: %v", err)
        }

	// create a map of space guid to space name and org name	
	spacemap := map[string]SpaceInfo{}
	for _, space := range spaces {
		spacemap[space.Guid] = SpaceInfo { Name:space.Name , OrgName: orgmap[space.OrganizationGuid] }
	} 

	// removed apps which aren't started from the list....this way we know the number of goroutines to wait for
	startedapps := apps[:0]
	for _, n := range apps {
 	   if n.State == "STARTED" {
 	       startedapps = append(startedapps, n)
 	   }
	}

	// split the app list into chunks to parallelize the appstats calls.  Use max of 10 goroutines
	chunkSize := 1000
	if len(startedapps) < 1000 {
                chunkSize = 100
        } else {
                chunkSize = len(startedapps) / 9 
        }

	for i := 0; i < len(startedapps); i += chunkSize {
		end := i + chunkSize

		if end > len(startedapps) {
			end = len(startedapps)
		}

		applistchunks = append(applistchunks, startedapps[i:end])
	}
	log.Printf("Found %v started apps, using chunksize of %v and %v goroutines", len(startedapps), chunkSize, len(applistchunks))
	
	// launch a thread for each chunk of X apps to create target lists in parallel and put them on the channel
	for _, chunk := range applistchunks {
                go func (client *cfclient.Client, chunk []cfclient.App, apiaddress string, spacemap map[string]SpaceInfo) {
			var tgroups []TargetGroup
			var targets []string
			
			for _, app := range chunk {
				stats,err := client.GetAppStats(app.Guid)
				if err != nil {
                			log.Printf("Error generating stats for app %v. %v", app.Name, err)
        			}
				for _, stat := range stats {
					route := stat.Stats.Host + ":" + strconv.Itoa(stat.Stats.Port) 
					targets = append(targets, route)
				}
				tgroups = append(tgroups, TargetGroup {
					Targets: targets,
					Labels: map[string]string{"job": app.Name, "cf": apiaddress, "space": spacemap[app.SpaceGuid].Name, "org": spacemap[app.SpaceGuid].OrgName},
				})
				targets = nil
			}
			target <- []TargetGroup(tgroups)
		}(client, chunk, apiaddress, spacemap)
	}

	//take the individual chunked appstats off the channel and combine, waiting for all chunks to complete
	for i := 0; i < len(applistchunks); i++ {
                select {
                case apptarget := <-target:
                    tgroups = append(tgroups,apptarget...)
                }
        }

	targetlist, err := json.MarshalIndent(tgroups, "", "  ")
        if err != nil {
        	log.Fatal(err)
        }

        log.Printf("Done generating target list")

	err = ioutil.WriteFile(*OutputFile, targetlist, 0644)
	if err !=  nil {
		log.Printf("File cant be written: %v", err)	
		os.Exit(1)
	}
	
	done <- true
}

func main() {
  flag.Parse()
  overrideFlagsWithEnvVars()
 
  var port string
  c := &cfclient.Config{
    ApiAddress:        *ApiAddress,
    ClientID:          *ClientID,
    ClientSecret:      *ClientSecret,
    SkipSslValidation: *SkipSSL,
  }
  client, err := cfclient.NewClient(c)
  if err != nil {
	log.Fatal("Error connecting to API: %s", err.Error())
	os.Exit(1)
  }

 done = make(chan bool)
 target = make(chan []TargetGroup)

 go updateTargetList(client, *ApiAddress)

 if port = os.Getenv("PORT"); len(port) == 0 {
        port = "8080" 
 }

 http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Cloud Foundry Target Generator for Prometehus Service Discovery")
    })

 log.Fatal(http.ListenAndServe(":" + port, nil))
}
