package taskexec

func readQueueToChannels(ctx context.Context, queue eventqueue.Reader, eventChan chan a2a.Event, errorChan chan error) {
	for {
		event, err := queue.Read(ctx)
		if err != nil {
			select {
			case errorChan <- err:
			case <-ctx.Done():
			}
			return
		}

		select {
		case eventChan <- event:
		case <-ctx.Done():
			return
		}
	}
}
