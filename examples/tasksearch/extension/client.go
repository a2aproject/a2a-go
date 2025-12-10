package tasksearchext

import (
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aext"
)

var ClientTaskSearch = a2aext.NewUnaryClientMethod[Request, Response](
	MethodName,
	a2aclient.NewJSONRPCExtensionBinding[Response](JSONRPCMethodName),
)
