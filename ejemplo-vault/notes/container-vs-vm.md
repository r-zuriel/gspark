# Containers versus virtual machines

A virtual machine emulates a whole computer with its own kernel, so it is heavier but strongly
isolated. A container shares the host kernel and packages only the application and its
dependencies, so it starts in milliseconds and uses far less memory.

Choose a VM when you need a different kernel or hard isolation; choose a container when you want
density, fast startup and reproducible application environments. Both can be orchestrated.
