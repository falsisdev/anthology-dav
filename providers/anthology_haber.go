package providers

func init() {
	Register(newLiveM3UProvider("anthology_haber", "Anthology_Haber", []string{"HABER"}, false))
}
