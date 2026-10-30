package calendar

// maxRecurrenceIterations bounds work spent on one calendar object. A rule
// with a large COUNT is indexed as unbounded so it stays in listing queries.
const maxRecurrenceIterations = 100000

// maxRecurrenceInstances bounds the response size for one calendar object.
const maxRecurrenceInstances = 10000
