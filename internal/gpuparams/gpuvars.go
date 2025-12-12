package gpuparams

var (
	// Labels represents the range of labels that can be used for test cases selection.
	Labels = []string{"nvidia-ci", Label}
)

// // ContainsLabel checks if a label exists in the provided slice
// func ContainsLabel(labels []string, label string) bool {
// 	for _, l := range labels {
// 		if l == label {
// 			return true
// 		}
// 	}
// 	return false
// }

// // ContainsAnyLabel checks if any of the provided labels exist in the slice
// func ContainsAnyLabel(labels []string, checkLabels ...string) bool {
// 	for _, l := range labels {
// 		for _, check := range checkLabels {
// 			if l == check {
// 				return true
// 			}
// 		}
// 	}
// 	return false
// }