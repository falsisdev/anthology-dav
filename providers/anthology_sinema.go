package providers

func init() {
	Register(newLiveM3UProvider("anthology_sinema", "Anthology_Sinema", []string{"SİNEMA", "SINEMA"}, false))
}
