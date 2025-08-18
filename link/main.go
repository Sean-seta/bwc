package main

import (
	"bwc/model"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ---------- API Response ----------
type SearchResponse struct {
	NextPageToken string `json:"nextPageToken"`
	Items         []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			ChannelID    string `json:"channelId"`
			ChannelTitle string `json:"channelTitle"`
		} `json:"snippet"`
	} `json:"items"`
}

type VideosResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Content struct {
			Duration string `json:"duration"` // ISO 8601
		} `json:"contentDetails"`
	} `json:"items"`
}

// ---------- Fetch YouTube Search ----------
func fetchVideos(apiKey, query, pageToken, duration string) (*SearchResponse, error) {
	escapedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/search?part=snippet&type=video&q=%s&maxResults=50&videoDuration=%s&key=%s",
		escapedQuery, duration, apiKey,
	)
	if pageToken != "" {
		apiURL += "&pageToken=" + pageToken
	}

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	var result SearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ---------- Fetch Video Details ----------
func fetchVideoDurations(apiKey string, ids []string) (map[string]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	idParam := strings.Join(ids, ",")
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?part=contentDetails&id=%s&key=%s", idParam, apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	var res struct {
		Items []struct {
			ID             string `json:"id"`
			ContentDetails struct {
				Duration string `json:"duration"` // ISO 8601 duration
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	durations := make(map[string]int) // videoID -> seconds
	for _, item := range res.Items {
		durations[item.ID] = parseISODuration(item.ContentDetails.Duration)
	}
	return durations, nil
}

// ---------- ISO8601 to seconds ----------
func parseISODuration(s string) int {
	var h, m, sec int
	fmt.Sscanf(s, "PT%dH%dM%dS", &h, &m, &sec)
	if h == 0 && m == 0 && sec == 0 {
		fmt.Sscanf(s, "PT%dM%dS", &m, &sec)
	}
	return h*3600 + m*60 + sec
}

// ---------- Store to DB ----------
func storeVideos(db *gorm.DB, videos []*model.Video) error {
	ids := make([]string, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.ID)
	}
	var existVideos []model.Video
	if err := db.Where("id IN ?", ids).Find(&existVideos).Error; err != nil {
		return err
	}
	existMap := make(map[string]struct{}, len(existVideos))
	for _, v := range existVideos {
		existMap[v.ID] = struct{}{}
	}

	newVideos := make([]*model.Video, 0, len(videos))
	for _, v := range videos {
		if _, exists := existMap[v.ID]; !exists {
			newVideos = append(newVideos, v)
		}
	}
	if len(newVideos) == 0 {
		return nil
	}
	fmt.Println("Storing", len(newVideos), "new videos to the database...")
	return db.Create(&newVideos).Error
}

// ---------- Main ----------
func main() {
	apiKey := "AIzaSyAuDnzxlxe50f-qTKQiyFOHoGU8NWgehng"
	//query := "Nature"
	//query := "Wildlife"
	//query := "nature wildlife forest"
	//query := "forest"
	query := "nature sounds"
	dsn := "host=34.132.252.7 user=user password=password dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Shanghai"

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}
	//db.AutoMigrate(&model.Video{})

	pageToken := ""
	for _, dur := range []string{"medium", "long"} { // >=4min automatically
		pageToken = ""
		for {
			result, err := fetchVideos(apiKey, query, pageToken, dur)
			if err != nil {
				log.Fatal(err)
			}

			var batch []*model.Video
			videoIDs := []string{}
			for _, item := range result.Items {
				if item.ID.VideoID == "" {
					continue
				}
				videoIDs = append(videoIDs, item.ID.VideoID)
			}

			durations, err := fetchVideoDurations(apiKey, videoIDs)
			if err != nil {
				log.Fatal(err)
			}

			for _, item := range result.Items {
				sec := durations[item.ID.VideoID]
				if sec >= 120 { // >=2min
					batch = append(batch, &model.Video{
						ID:       item.ID.VideoID,
						URL:      fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID.VideoID),
						Title:    item.Snippet.Title,
						Duration: (time.Duration(sec) * time.Second).String(), // correct conversion
						Status:   model.VideoStatusPending,
						Prefix:   model.VideoPrefixNature,
					})
				}
			}

			if len(batch) > 0 {
				if err := storeVideos(db, batch); err != nil {
					log.Printf("error storing videos: %s", err)
				}
			}

			if result.NextPageToken == "" {
				break
			}
			pageToken = result.NextPageToken
			time.Sleep(2 * time.Second)
		}
	}
}
