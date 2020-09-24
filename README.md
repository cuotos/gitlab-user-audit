# Check Gitlab Users

This Go script will print the permissions given to any users THAT ARE NOT INHERITED FROM ABOVE.

It will print if the permission is assigned to a group or project and the users details.  
It also prints a link directly to the admin page so the permissions can be removed if required.


## Why?

User permissions assigned at the top level group will be inherited by any projects and groups below it.  
But if a user is also given permissions to a lower group or project, this permissions are linked TO THAT project or group.

When the user is then removed from the top level members list, all inherited permissions will be removed, but any  
permissions assigned explicitly will be persisted.

There is no easy way to view all these permissions in Gitlab....