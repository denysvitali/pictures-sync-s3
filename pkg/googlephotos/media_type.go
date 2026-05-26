package googlephotos

func IsVideo(filename string) bool {
	ext := lowerExt(filename)
	return videoExts[ext]
}

var videoExts = map[string]bool{
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
	".wmv": true, ".flv": true, ".m4v": true, ".3gp": true,
}
