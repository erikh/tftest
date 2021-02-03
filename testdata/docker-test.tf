terraform {
  required_providers {
    docker = {
      source = "kreuzwerker/docker"
    }
  }
}

provider "docker" {}

resource "docker_network" "private_network" {
  name = "my_network"
}

resource "docker_image" "zerotier" {
  name         = "bltavares/zerotier"
  keep_locally = true
}

resource "docker_container" "uno" {
  name  = "zerotier_uno"
  image = docker_image.zerotier.latest
  upload {
    source = "/etc/issue" # this is a file on your filesystem
    file   = "/etc/motd"
  }
  upload {
    content = "this is my issue file" # this is literal content in the file
    file    = "/etc/issue"
  }
}

resource "docker_container" "dos" {
  name  = "zerotier_dos"
  image = docker_image.zerotier.latest
}
