package curseforge

func selectLatestReleaseFile(files []fileResponse) *fileResponse {
	var latest *fileResponse
	for i := range files {
		candidate := &files[i]
		if !candidate.IsAvailable || candidate.ReleaseType != 1 {
			continue
		}
		if latest == nil || candidate.FileDate > latest.FileDate {
			latest = candidate
		}
	}
	return latest
}

func selectFileByVersion(files []fileResponse, version string) *fileResponse {
	for i := range files {
		candidate := &files[i]
		if !candidate.IsAvailable {
			continue
		}
		if candidate.DisplayName == version || candidate.FileName == version {
			return candidate
		}
	}
	return nil
}
