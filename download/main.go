package main

import (
	"bwc/model"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	bucket = "s3-vdr-amazon-delivery-or-1" // replace with your S3 bucket name
	dsn    = "host=34.132.252.7 user=user password=password dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Shanghai"
)

func connectDB() *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to DB:", err)
	}
	//db.AutoMigrate(&model.Video{})
	return db
}

// ---------- Download using yt-dlp ----------
func downloadVideo(url string) (string, error) {
	cookiePath := "./download/cookies_fpt_phong"
	outputTemplate := "%(id)s.%(ext)s"

	cmd := exec.Command(
		"yt-dlp",
		"--match-filter", "duration>120",
		"--cookies", cookiePath,
		"--write-info-json",
		"-o", outputTemplate,
		"--sleep-requests", "5",
		"--sleep-interval", "30",
		"--extractor-args", "youtube:formats=duplicate",
		"-f", "bv[protocol=sabr][height>=720][height<=1080]+ba[protocol=sabr]/bestvideo[protocol=m3u8][height>=720][height<=1080]+bestaudio[protocol=m3u8]/(bestvideo[ext=mp4][height>=720][height<=1080]+bestaudio)/(best[ext=mp4][height>=720][height<=1080])",
		"--remux-video", "mp4",
		url,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Find the actual file
	files, _ := filepath.Glob("*.mp4")
	if len(files) == 0 {
		return "", fmt.Errorf("downloaded file not found")
	}
	return files[0], nil
}

// ---------- Upload to S3 ----------
func uploadToS3(file string, prefix string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	info := base + ".info.json"

	// Upload video
	fmt.Printf("Upload to S3: s3://%s/%s/%s\n", bucket, prefix, filepath.Base(file))
	runCmd("aws", "s3", "cp", "--", file, fmt.Sprintf("s3://%s/%s/%s", bucket, prefix, filepath.Base(file)))

	// Upload info JSON
	fmt.Printf("Upload to S3: s3://%s/%s/%s\n", bucket, prefix, filepath.Base(info))
	runCmd("aws", "s3", "cp", "--", info, fmt.Sprintf("s3://%s/%s/%s", bucket, prefix, filepath.Base(info)))

	// Clean up local files safely
	for _, f := range []string{file, info} {
		if err := os.Remove(f); err != nil {
			fmt.Println("Warning: failed to delete file:", f, err)
		}
	}

	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s/%s", bucket, prefix, filepath.Base(file)), nil
}

func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Fatal("Command failed:", err, "Command:", name, strings.Join(args, " "))
	}
}

func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	return f
}

// ---------- Worker ----------
func processPendingVideos(db *gorm.DB, bucket string) {
	for {
		var video model.Video
		// Get one pending video
		result := db.Where("status = ?", model.VideoStatusPending).First(&video)
		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				fmt.Println("No more pending videos. Exiting.")
				continue
			}
			log.Println("DB error:", result.Error)
			continue
		}
		// Update status to cooking
		err := db.Model(&model.Video{}).Where("id = ?", video.ID).Updates(map[string]interface{}{
			"status":   model.VideoStatusCooking,
			"start_at": time.Now(),
		}).Error
		if err != nil {
			log.Println("Failed to update video status:", err)
			continue
		}
		fmt.Println("Processing video:", video.Title)

		// Download
		filePath, err := downloadVideo(video.URL)
		if err != nil {
			fmt.Println("❌ Download failed:", err)
			db.Model(&video).Update("status", model.VideoStatusFailed)
			continue
		}

		// Upload
		s3url, err := uploadToS3(filePath, string(video.Prefix))
		if err != nil {
			fmt.Println("❌ S3 upload failed:", err)
			db.Model(&video).Update("status", model.VideoStatusFailed)
			//os.Remove(filePath)
			continue
		}

		// Update DB
		db.Model(&model.Video{}).
			Where("id = ?", video.ID).
			Updates(model.Video{
				Status: model.VideoStatusCompleted,
				EndAt:  time.Now(),
			})
		fmt.Println("✅ Uploaded to S3:", s3url)

		// Clean up local file
		os.Remove(filePath)

		// Optional: pause a bit
		time.Sleep(2 * time.Second)
	}
}

func main() {
	checkYtDlpVersion()

	db := connectDB()
	bucket := "your-s3-bucket"
	processPendingVideos(db, bucket)
}
func checkYtDlpVersion() {
	cmd := exec.Command("yt-dlp", "--version")
	out, err := cmd.Output()
	if err != nil {
		log.Println("Failed to get yt-dlp version:", err)
		return
	}
	fmt.Println("yt-dlp version:", strings.TrimSpace(string(out)))
}
