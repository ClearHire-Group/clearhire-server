package groqadapter

import "time"

// Indireção mínima só para os testes não dependerem do relógio real.
var (
	timeNow   = time.Now
	timeSince = time.Since
)
