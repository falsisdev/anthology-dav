package providers

func init() {
	Register(newLiveM3UProvider("anthology_ulusal", "Anthology_Ulusal_TV", []string{"ULUSAL"}, false))
}
