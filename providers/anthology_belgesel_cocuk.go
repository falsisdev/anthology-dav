package providers

func init() {
	Register(newLiveM3UProvider("anthology_belgesel_cocuk", "Anthology_Belgesel_ve_Cocuk", []string{"BELGESEL", "ÇOCUK", "COCUK"}, false))
}
