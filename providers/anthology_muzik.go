package providers

func init() {
	Register(newLiveM3UProvider("anthology_muzik", "Anthology_Muzik_ve_Eglence", []string{"MÜZİK", "MUZIK", "EĞLENCE", "EGLENCE"}, false))
}
