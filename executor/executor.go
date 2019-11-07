package executor

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	empty "github.com/golang/protobuf/ptypes/empty"
	"github.com/packethost/rover/db"
	pb "github.com/packethost/rover/protos/rover"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetWorkflowContexts implements rover.GetWorkflowContexts
func GetWorkflowContexts(context context.Context, req *pb.WorkflowContextRequest, sdb *sql.DB) (*pb.WorkflowContextList, error) {
	if len(req.WorkerId) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "worker_id is invalid")
	}
	wfs, _ := db.GetfromWfWorkflowTable(context, sdb, req.WorkerId)
	if wfs == nil {
		return nil, status.Errorf(codes.InvalidArgument, "Worker not found for any workflows")
	}

	wfContexts := []*pb.WorkflowContext{}

	for _, wf := range wfs {
		wfContext, err := db.GetWorkflowContexts(context, sdb, wf)
		if err != nil {
			return nil, status.Errorf(codes.Aborted, "Invalid workflow %s found for worker %s", wf, req.WorkerId)
		}
		wfContexts = append(wfContexts, wfContext)
	}

	return &pb.WorkflowContextList{
		WorkflowContexts: wfContexts,
	}, nil
}

// GetWorkflowActions implements rover.GetWorkflowActions
func GetWorkflowActions(context context.Context, req *pb.WorkflowActionsRequest, sdb *sql.DB) (*pb.WorkflowActionList, error) {
	wfID := req.GetWorkflowId()
	if len(wfID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "workflow_id is invalid")
	}
	actions, err := db.GetWorkflowActions(context, sdb, wfID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "workflow_id is invalid")
	}
	return actions, nil
}

// ReportActionStatus implements rover.ReportActionStatus
func ReportActionStatus(context context.Context, req *pb.WorkflowActionStatus, sdb *sql.DB) (*empty.Empty, error) {
	wfID := req.GetWorkflowId()
	if len(wfID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "workflow_id is invalid")
	}
	if len(req.GetTaskName()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "task_name is invalid")
	}
	if len(req.GetActionName()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "action_name is invalid")
	}
	fmt.Printf("Received action status: %s\n", req)
	wfContext, err := db.GetWorkflowContexts(context, sdb, wfID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Workflow context not found for workflow %s", wfID)
	}
	wfActions, err := db.GetWorkflowActions(context, sdb, wfID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Workflow actions not found for workflow %s", wfID)
	}

	// We need bunch of checks here considering
	// Considering concurrency and network latencies & accuracy for proceeding of WF
	actionIndex := wfContext.GetCurrentActionIndex()
	if req.GetActionStatus() == pb.ActionState_ACTION_IN_PROGRESS {
		if wfContext.GetCurrentAction() != "" {
			actionIndex = actionIndex + 1
		}
	}
	action := wfActions.ActionList[actionIndex]
	if action.GetTaskName() != req.GetTaskName() {
		return nil, status.Errorf(codes.FailedPrecondition, "Reported task name not matching in actions info")
	}
	if action.GetName() != req.GetActionName() {
		return nil, status.Errorf(codes.FailedPrecondition, "Reported action name not matching in actions info")
	}
	wfContext.CurrentWorker = action.GetWorkerId()
	wfContext.CurrentTask = req.GetTaskName()
	wfContext.CurrentAction = req.GetActionName()
	wfContext.CurrentActionState = req.GetActionStatus()
	wfContext.CurrentActionIndex = actionIndex
	err = db.UpdateWorkflowState(context, sdb, wfContext)
	if err != nil {
		return &empty.Empty{}, fmt.Errorf("Failed to update the workflow_state table. Error : %s", err)
	}
	// TODO the below "time" would be a part of the request which is coming form worker.
	time := time.Now()
	err = db.InsertIntoWorkflowEventTable(context, sdb, req, time)
	if err != nil {
		return &empty.Empty{}, fmt.Errorf("Failed to update the workflow_event table. Error : %s", err)
	}
	fmt.Printf("Current context %s\n", wfContext)
	return &empty.Empty{}, nil
}