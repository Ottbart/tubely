package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	//limit size of incoming video to 1GB
	const maxBytes = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	//authenticate user to get userID
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// check if the user is the owner of the video
	meta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video ID from database", err)
		return
	}
	if meta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "user is not owner of requested video", errors.New("user ID did not match"))
		return
	}

	//parse uploaded video
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// check the media type of the file
	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid MIME type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type. Only MP4 is allowed", nil)
		return
	}

	//save uploaded file temporary on disk
	localFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating file: ", err)
		return
	}
	defer os.Remove(localFile.Name())
	defer localFile.Close()

	//copy file in local temp file
	if _, err := io.Copy(localFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error storing local copy of file: ", err)
		return
	}

	//process for fast start and discard old version
	processedPath, err := processVideoForFastStart(localFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error processing fast start: ", err)
		return
	}
	processedFile, err := os.Open(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error opening rocessed file: ", err)
		return
	}
	defer os.Remove(processedFile.Name())
	defer processedFile.Close()

	//check aspect ratio
	prefix := "other/"
	aspectRatio, err := getVideoAspectRatio(localFile.Name())
	if aspectRatio == "16:9" {
		prefix = "landscape/"
	} else if aspectRatio == "9:16" {
		prefix = "portrait/"
	}

	//give the file a name
	const extension = ".mp4" //only allowed video format and already checked with MIME type
	randKey := make([]byte, 32)
	if _, err := rand.Read(randKey); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error generating filename: ", err)
		return
	}
	filename := prefix + hex.EncodeToString(randKey) + extension

	//put object into S3
	putObject := s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(filename),
		Body:        processedFile,
		ContentType: &mediaType,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &putObject)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error uploading to aws: ", err)
		return
	}

	//store video link in database
	videoURL := fmt.Sprintf("https://%v/%v", cfg.s3CfDistribution, filename)
	meta.VideoURL = &videoURL
	if err := cfg.db.UpdateVideo(meta); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	//return video
	respondWithJSON(w, http.StatusOK, meta)
}
