# Security Context Constraints for Openshift (SCC)

We need to define that scribe mover pods are allowed to run as root,
which is typically needed to preserve the source UID's in the rsync/rclone target.

In openshift this is done using SCC's - see the [SCC docs here](
    https://docs.openshift.com/container-platform/4.6/authentication/managing-security-context-constraints.html
) and this [blog here](
    https://www.openshift.com/blog/managing-sccs-in-openshift
).

## SCC admission controller

SCC selection is dynamically computed for Pod's being created
based on the set of available SCC's, priorities, and RBAC rules.

For the gory details refer to:

1. [SCC admittion controller source code](
    https://github.com/openshift/apiserver-library-go/blob/release-4.6/pkg/securitycontextconstraints/sccadmission/admission.go#L79
)

1. [SCC sorting code](
    https://github.com/openshift/apiserver-library-go/blob/release-4.6/pkg/securitycontextconstraints/util/sort/bypriority.go
) - uses 3 levels of sub sorting by \[priority,restrictedScore,name\],

1. [Trying SCC's one by one until one is applied to the new Pod](
    https://github.com/openshift/apiserver-library-go/blob/release-4.6/pkg/securitycontextconstraints/sccadmission/admission.go#L193-L231
) - this will skip SCC's that cannot be applied and try the next SCC in order.

1. [Assign security policy validation code](
    https://github.com/openshift/apiserver-library-go/blob/release-4.6/pkg/securitycontextconstraints/sccmatching/matcher.go#L109-L112
) - checks that every constrain can be applied to the pod.

## Custom SCC with high priority

In the following commit we saw that there are nasty side effects
just to the existance of a higher priority than `anyuid`,
so that's one area we should watch out for.

[ocs-operator revert SCC priority](
    https://github.com/openshift/ocs-operator/commit/14b78266a867a6332180e1d665d44980f541908b
)

## Custom SCC with nil priority

Based on the admission controller behavior
to keep trying the next SCC's until one is applied,
we can provide a custom SCC with nil priority
and then make sure the pods specify a securityContext with `runAsUser: 0`,
which makes the admission controller to skip the `restricted` SCC,
and pick one of the next SCC's that can be applied to our pod,
most likely our custom one, but it can also be any other that applies.

The current folder contains a sample custom SCC with a nil priority,
and also SA, Role, RoleBinding and a Pod to test how this SCC is applied
to pods that require a securityContext with `runAsUser: 0`.

Try it with the script below - if the pod was assigned with the custom
SCC named `test-scc` then it worked as expected.

```
$ oc create -f config/scc/scc.yaml
securitycontextconstraints.security.openshift.io/test-scc created

$ oc create -f config/scc/sa.yaml
serviceaccount/test-scc-sa created

$ oc create -f config/scc/role.yaml
role.rbac.authorization.k8s.io/test-scc-role created

$ oc create -f config/scc/rolebinding.yaml
rolebinding.rbac.authorization.k8s.io/test-scc-rolebinding created

# create the pod by impersonating our test SA to avoid having admin permissions
$ oc create -f config/scc/pod.yaml --as system:serviceaccount:<<CURRENT-NAMESPACE>>:test-scc-sa
pod/test-scc-pod created

$ oc get -f config/scc/pod.yaml -o yaml | grep scc:
    openshift.io/scc: test-scc ✅

$ oc delete -f config/scc/
role.rbac.authorization.k8s.io "test-scc-role" deleted
rolebinding.rbac.authorization.k8s.io "test-scc-rolebinding" deleted
serviceaccount "test-scc-sa" deleted
securitycontextconstraints.security.openshift.io "test-scc" deleted
pod "test-scc-pod" deleted
```
