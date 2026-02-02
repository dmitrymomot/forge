package htmx

// SwapStrategy defines how HTMX should swap content into the target element.
type SwapStrategy string

const (
	SwapInnerHTML   SwapStrategy = "innerHTML"   // Replace the inner html of the target element
	SwapOuterHTML   SwapStrategy = "outerHTML"   // Replace the entire target element with the response
	SwapBeforeBegin SwapStrategy = "beforebegin" // Insert before the target element
	SwapAfterBegin  SwapStrategy = "afterbegin"  // Insert before the first child of the target element
	SwapBeforeEnd   SwapStrategy = "beforeend"   // Insert after the last child of the target element
	SwapAfterEnd    SwapStrategy = "afterend"    // Insert after the target element
	SwapDelete      SwapStrategy = "delete"      // Delete the target element
	SwapNone        SwapStrategy = "none"        // Do not swap content
)
