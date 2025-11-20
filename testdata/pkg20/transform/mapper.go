package transform

type Mapper func(interface{}) interface{}

func MapSlice(slice []interface{}, mapper Mapper) []interface{} {
	result := make([]interface{}, len(slice))
	for i, item := range slice {
		result[i] = mapper(item)
	}
	return result
}

func FilterSlice(slice []interface{}, predicate func(interface{}) bool) []interface{} {
	var result []interface{}
	for _, item := range slice {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

func ReduceSlice(slice []interface{}, reducer func(interface{}, interface{}) interface{}, initial interface{}) interface{} {
	accumulator := initial
	for _, item := range slice {
		accumulator = reducer(accumulator, item)
	}
	return accumulator
}

