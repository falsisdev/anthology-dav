package providers

func init() {
	Register(newLiveM3UProvider("anthology_spor", "Anthology_Spor", []string{"SPOR"}, true))
}
