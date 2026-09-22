package application

type Service struct {
	Work  UnitOfWork
	Clock Clock
}

func NewService(work UnitOfWork, clock Clock) *Service {
	return &Service{Work: work, Clock: clock}
}

const serviceName = "accountservice"
