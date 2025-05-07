package asyntask

type TaskQueue struct {
	queue map[string]Task
}

type Task struct {
	taskid   string
	question string
}
