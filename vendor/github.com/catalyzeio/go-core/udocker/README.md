µDocker
=======
A minimal Docker client built as a thin layer over on `net/http`.

The current set of Go Docker clients have some annoying issues, particularly
with handling streaming output and canceling long-running operations such as
builds. Users of this client library should convert to one of the full-featured
libraries once those problems are resolved.
