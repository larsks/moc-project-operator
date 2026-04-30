The Mass Open Cloud project hosts a shared OpenShift instance for research and academic clients. A project (such as a class, or a research group) is assigned a namespace in the cluster. This repository implements a mechanism for managing these allocations:

When the operator needs to allocate resources for a group, they create a Project resource. This needs to be created in the namespace in which the controller is running. The manifest used to deploy the controller will need to use the downward api to expose the namespace name as an environment variable.

When a Project manifest is created or modified:

1. Ensuring that a namespace exists matching the project name. The namespace should have the `project.massopen.cloud/name` label set to the project name.
2. Ensuring that a group exists matching the project name and that its members match the value of project.spec.members.
3. Ensuring that a RoleBinding exists in the project namespace mapping the project group to the "edit" role.
4. Ensuring that a limitrange exists in the project namespace with the limits from project.spec.limits.
5. Ensuring that a resourcequota exists in the project with the quotas from project.spec.quotas

The controller should automatically reconcile all these resources so they always match the values from the Project manifest.

When a Project manifest is deleted:

1. The controller should set the `project.massopen.cloud/deleted` label on the namespace to "true".
1. If project.spec.deleteAction is DeleteActionIgnore, the controller should perform no other actions.
1. If project.spec.deleteAction is DeleteActionPreserve, then controller should delete the project group, but should not delete any resources from the project namespace.
1. If project.spec.deleteAction is DeleteActionDelete, the controller should delete the project namespace and all its resources.

